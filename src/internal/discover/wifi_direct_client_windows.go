//go:build windows && amd64

package discover

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/enumeration"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/wifidirect"

	"clipshare/src/internal/log"
)

const (
	wifiDirectClientRetry = 5 * time.Second
	wifiDirectClientWatch = 5 * time.Second
)

// runPlatformWiFiDirectClient joins the phone group owner named peerName as a
// Wi-Fi Direct client and reports its IP to onOwner each time a connection is
// established. It reconnects when the link drops, until ctx is done.
func runPlatformWiFiDirectClient(ctx context.Context, peerName string, onOwner func(ownerIP string)) {
	// WinRT objects are apartment-bound, so keep this goroutine on one OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := winrt.Initialize(); err != nil {
		log.Warnf("wifi direct client: initialize WinRT: %v", err)
		return
	}
	defer winrt.Uninitialize()

	for {
		if ctx.Err() != nil {
			return
		}
		device, ip, err := connectToGroupOwner(peerName)
		if err != nil {
			log.Warnf("wifi direct client: %v (retry in %s)", err, wifiDirectClientRetry)
			select {
			case <-ctx.Done():
				return
			case <-time.After(wifiDirectClientRetry):
			}
			continue
		}
		log.Infof("wifi direct client: joined %s, group owner %s", peerName, ip)
		onOwner(ip)
		watchGroupOwner(ctx, device)
		log.Infof("wifi direct client: link to %s dropped", peerName)
	}
}

// connectToGroupOwner finds the phone by its Wi-Fi Direct device name and
// connects to it. The returned device must be kept referenced for the link to
// stay up.
func connectToGroupOwner(peerName string) (*wifidirect.IWiFiDirectDevice, string, error) {
	selectorStatics, err := wifidirect.WiFiDirectDeviceStatics2()
	if err != nil {
		return nil, "", fmt.Errorf("device statics: %w", err)
	}
	// AssociationEndpoint lists Wi-Fi Direct peers that are not yet connected,
	// which is the case for the phone group owner before this PC joins it.
	selector, err := selectorStatics.GetDeviceSelector(wifidirect.WiFiDirectDeviceSelectorTypeAssociationEndpoint)
	if err != nil {
		return nil, "", fmt.Errorf("device selector: %w", err)
	}

	infoStatics, err := enumeration.DeviceInformationStatics()
	if err != nil {
		return nil, "", fmt.Errorf("device information statics: %w", err)
	}
	op, err := infoStatics.FindAllAsyncAqsFilter(selector)
	if err != nil {
		return nil, "", fmt.Errorf("find devices: %w", err)
	}
	insp, err := op.Await()
	if err != nil {
		return nil, "", fmt.Errorf("await devices: %w", err)
	}
	view, err := winrt.QueryInterface[enumeration.IVectorViewOfDeviceInformation](
		unsafe.Pointer(insp),
		&enumeration.IID_IVectorViewOfDeviceInformation,
	)
	if err != nil {
		return nil, "", fmt.Errorf("device list: %w", err)
	}
	size, err := view.Size()
	if err != nil {
		return nil, "", fmt.Errorf("device count: %w", err)
	}
	seen := make([]string, 0, size)
	for i := uint32(0); i < size; i++ {
		info, err := view.GetAt(i)
		if err != nil {
			continue
		}
		name, err := info.Name()
		if err != nil {
			continue
		}
		seen = append(seen, name)
		if name != peerName {
			continue
		}
		id, err := info.Id()
		if err != nil {
			return nil, "", fmt.Errorf("device id for %s: %w", peerName, err)
		}
		return connectDevice(id)
	}
	return nil, "", fmt.Errorf("Wi-Fi Direct peer %q not found (visible devices: %q)", peerName, seen)
}

// connectDevice opens the Wi-Fi Direct link to a device id as a client
// (group-owner intent 0) and returns the device and the owner IP.
func connectDevice(deviceID string) (*wifidirect.IWiFiDirectDevice, string, error) {
	params, err := wifidirect.NewWiFiDirectConnectionParameters()
	if err != nil {
		return nil, "", fmt.Errorf("connection parameters: %w", err)
	}
	if err := params.SetGroupOwnerIntent(0); err != nil {
		return nil, "", fmt.Errorf("group owner intent: %w", err)
	}
	params2, err := params.AsWiFiDirectConnectionParameters2()
	if err != nil {
		return nil, "", fmt.Errorf("pairing interface: %w", err)
	}
	if err := params2.SetPreferredPairingProcedure(wifidirect.WiFiDirectPairingProcedureGroupOwnerNegotiation); err != nil {
		return nil, "", fmt.Errorf("pairing procedure: %w", err)
	}

	statics, err := wifidirect.WiFiDirectDeviceStatics2()
	if err != nil {
		return nil, "", fmt.Errorf("device statics: %w", err)
	}
	op, err := statics.FromIdAsync(deviceID, &params.IWiFiDirectConnectionParameters)
	if err != nil {
		return nil, "", fmt.Errorf("connect: %w", err)
	}
	device, err := op.Await()
	if err != nil {
		return nil, "", fmt.Errorf("connect await: %w", err)
	}
	if device == nil {
		return nil, "", errors.New("connect returned no device")
	}

	pairs, err := device.GetConnectionEndpointPairs()
	if err != nil {
		device.Release()
		return nil, "", fmt.Errorf("endpoint pairs: %w", err)
	}
	count, err := pairs.Size()
	if err != nil || count == 0 {
		device.Release()
		return nil, "", errors.New("no endpoint pairs for connected device")
	}
	pair, err := pairs.GetAt(0)
	if err != nil {
		device.Release()
		return nil, "", fmt.Errorf("endpoint pair: %w", err)
	}
	host, err := pair.RemoteHostName()
	if err != nil {
		device.Release()
		return nil, "", fmt.Errorf("remote host: %w", err)
	}
	ip, err := host.RawName()
	if err != nil || ip == "" {
		device.Release()
		return nil, "", errors.New("group owner address missing")
	}
	return device, ip, nil
}

// watchGroupOwner blocks until the Wi-Fi Direct link drops or ctx is done.
func watchGroupOwner(ctx context.Context, device *wifidirect.IWiFiDirectDevice) {
	defer device.Release()
	ticker := time.NewTicker(wifiDirectClientWatch)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status, err := device.ConnectionStatus()
			if err != nil || status != wifidirect.WiFiDirectConnectionStatusConnected {
				return
			}
		}
	}
}
