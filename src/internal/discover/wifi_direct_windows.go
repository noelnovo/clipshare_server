//go:build windows && amd64

package discover

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/enumeration"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/wifidirect"

	"clipshare/src/internal/config"
	"clipshare/src/internal/log"
)

const (
	// incomingPeerAttempts bounds how often a connection request is resolved
	// through WinRT before the request is abandoned.
	incomingPeerAttempts = 5
	// incomingPeerRetryDelay is the wait between resolution attempts.
	incomingPeerRetryDelay = time.Second
)

// startPlatformWiFiDirect advertises the Windows host as a discoverable
// autonomous group owner. Android's WifiP2pManager discovers this peer and
// initiates group formation; the existing daemon then serves WebSocket traffic
// on the P2P interface.
func startPlatformWiFiDirect(ctx context.Context, cfg *config.Config) error {
	if !wifiDirectAllowed(cfg) {
		if cfg.WiFiDirect.Enabled {
			log.Warnf("wifi direct: disabled because [tls].enabled = false; Wi-Fi Direct requires mutual TLS")
		}
		return nil
	}
	go runWindowsWiFiDirect(ctx)
	return nil
}

func runWindowsWiFiDirect(ctx context.Context) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := winrt.Initialize(); err != nil {
		log.Warnf("wifi direct: initialize Windows Runtime: %v", err)
		return
	}

	publisher, err := wifidirect.NewWiFiDirectAdvertisementPublisher()
	if err != nil {
		log.Warnf("wifi direct: create Windows advertiser: %v", err)
		return
	}
	defer publisher.Release()

	advertisement, err := publisher.Advertisement()
	if err != nil {
		log.Warnf("wifi direct: get Windows advertisement: %v", err)
		return
	}
	defer advertisement.Release()
	if err := advertisement.SetIsAutonomousGroupOwnerEnabled(true); err != nil {
		log.Warnf("wifi direct: enable autonomous group owner: %v", err)
		return
	}
	if err := advertisement.SetListenStateDiscoverability(
		wifidirect.WiFiDirectAdvertisementListenStateDiscoverabilityNormal,
	); err != nil {
		log.Warnf("wifi direct: enable peer discoverability: %v", err)
		return
	}
	advertisement2, err := winrt.QueryInterface[wifidirect.IWiFiDirectAdvertisement2](
		unsafe.Pointer(advertisement),
		&wifidirect.IID_IWiFiDirectAdvertisement2,
	)
	if err != nil {
		log.Warnf("wifi direct: get pairing-method settings: %v", err)
		return
	}
	defer advertisement2.Release()
	methods, err := advertisement2.SupportedConfigurationMethods()
	if err != nil {
		log.Warnf("wifi direct: get supported pairing methods: %v", err)
		return
	}
	if err := methods.Clear(); err != nil {
		methods.Release()
		log.Warnf("wifi direct: clear default pairing methods: %v", err)
		return
	}
	if err := methods.Append(wifidirect.WiFiDirectConfigurationMethodPushButton); err != nil {
		methods.Release()
		log.Warnf("wifi direct: require push-button pairing: %v", err)
		return
	}
	methods.Release()

	var peersMu sync.Mutex
	var peers []*wifidirect.IWiFiDirectDevice
	shuttingDown := false
	defer func() {
		peersMu.Lock()
		defer peersMu.Unlock()
		shuttingDown = true
		for _, peer := range peers {
			peer.Release()
		}
	}()

	statusHandler, err := wifidirect.NewTypedEventHandlerOfWiFiDirectAdvertisementPublisherAndWiFiDirectAdvertisementPublisherStatusChangedEventArgs(
		func(_ *wifidirect.IWiFiDirectAdvertisementPublisher, args *wifidirect.IWiFiDirectAdvertisementPublisherStatusChangedEventArgs) {
			status, statusErr := args.Status()
			if statusErr != nil {
				log.Warnf("wifi direct: read Windows advertiser status: %v", statusErr)
				return
			}
			if status == wifidirect.WiFiDirectAdvertisementPublisherStatusAborted {
				failure, failureErr := args.Error()
				if failureErr != nil {
					log.Warnf("wifi direct: Windows advertiser aborted: %v", failureErr)
					return
				}
				log.Warnf("wifi direct: Windows advertiser aborted: %s", failure)
				return
			}
			log.Infof("wifi direct: Windows peer advertiser status: %s", status)
		},
	)
	if err != nil {
		log.Warnf("wifi direct: create advertiser status handler: %v", err)
		return
	}
	defer statusHandler.Close()
	statusToken, err := publisher.AddStatusChanged(statusHandler)
	if err != nil {
		log.Warnf("wifi direct: subscribe to Windows advertiser status: %v", err)
		return
	}
	defer publisher.RemoveStatusChanged(statusToken)

	listener, err := wifidirect.NewWiFiDirectConnectionListener()
	if err != nil {
		log.Warnf("wifi direct: create Windows connection listener: %v", err)
		return
	}
	defer listener.Release()

	connectionHandler, err := wifidirect.NewTypedEventHandlerOfWiFiDirectConnectionListenerAndWiFiDirectConnectionRequestedEventArgs(
		func(_ *wifidirect.IWiFiDirectConnectionListener, args *wifidirect.IWiFiDirectConnectionRequestedEventArgs) {
			request, err := args.GetConnectionRequest()
			if err != nil {
				log.Warnf("wifi direct: get incoming Windows connection request: %v", err)
				return
			}
			information, err := request.DeviceInformation()
			if err != nil {
				request.Release()
				log.Warnf("wifi direct: get connecting peer information: %v", err)
				return
			}
			// Ownership of request and information moves to the goroutine.
			// Windows invalidates both as soon as this callback returns, so they
			// must stay referenced until FromIdAsync has completed.
			go handleIncomingPeer(ctx, request, information, &peersMu, &peers, &shuttingDown)
		},
	)
	if err != nil {
		log.Warnf("wifi direct: create Windows connection handler: %v", err)
		return
	}
	defer connectionHandler.Close()
	connectionToken, err := listener.AddConnectionRequested(connectionHandler)
	if err != nil {
		log.Warnf("wifi direct: listen for incoming peers: %v", err)
		return
	}
	defer listener.RemoveConnectionRequested(connectionToken)

	if err := publisher.Start(); err != nil {
		log.Warnf("wifi direct: start Windows peer advertiser: %v", err)
		return
	}
	status, err := publisher.Status()
	if err != nil {
		log.Warnf("wifi direct: read Windows advertiser startup status: %v", err)
	} else {
		log.Infof("wifi direct: Windows autonomous group-owner advertisement requested (status=%s)", status)
	}

	<-ctx.Done()
	if err := publisher.Stop(); err != nil {
		log.Warnf("wifi direct: stop Windows peer advertiser: %v", err)
	}
}

// handleIncomingPeer completes an incoming Wi-Fi Direct connection request.
//
// Windows is the autonomous group owner. Before the peer can be opened with
// WiFiDirectDevice.FromIdAsync it has to be paired: Android's WifiP2pManager
// uses WPS push-button, which Windows surfaces as a ConfirmOnly pairing
// ceremony. Skipping that step makes FromIdAsync fail with E_ELEMENT_NOT_FOUND
// (0x80070490) even though the connection request itself was received.
//
// request and information are owned by this goroutine (the event callback
// transfers them) and must stay referenced until the asynchronous operations
// finish; Windows invalidates them when the callback returns.
func handleIncomingPeer(
	ctx context.Context,
	request *wifidirect.IWiFiDirectConnectionRequest,
	information *enumeration.IDeviceInformation,
	peersMu *sync.Mutex,
	peers *[]*wifidirect.IWiFiDirectDevice,
	shuttingDown *bool,
) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer request.Release()
	defer information.Release()

	name, _ := information.Name()
	deviceID, err := information.Id()
	if err != nil {
		log.Warnf("wifi direct: get connecting peer ID: %v", err)
		return
	}
	log.Infof("wifi direct: incoming connection request from %q (%s)", name, deviceID)

	info2, err := winrt.QueryInterface[enumeration.IDeviceInformation2](
		unsafe.Pointer(information),
		&enumeration.IID_IDeviceInformation2,
	)
	if err != nil {
		log.Warnf("wifi direct: inspect connecting peer: %v", err)
		return
	}
	defer info2.Release()

	pairing, err := info2.Pairing()
	if err != nil {
		log.Warnf("wifi direct: get pairing interface: %v", err)
		return
	}
	defer pairing.Release()

	paired, err := pairing.IsPaired()
	if err != nil {
		log.Warnf("wifi direct: read pairing state: %v", err)
	}
	if !paired {
		if err := pairIncomingPeer(pairing); err != nil {
			// The AEP pairing state is not always reliable for autonomous GO
			// reconnects. Let FromIdAsync try anyway; if the peer is already
			// paired at the radio level the data path still comes up.
			log.Warnf("wifi direct: pair %q: %v (continuing)", name, err)
		} else {
			log.Infof("wifi direct: paired with %q", name)
		}
	} else {
		log.Infof("wifi direct: peer %q already paired", name)
	}

	statics, err := wifidirect.WiFiDirectDeviceStatics()
	if err != nil {
		log.Warnf("wifi direct: open Windows peer statics: %v", err)
		return
	}
	defer statics.Release()

	var peer *wifidirect.IWiFiDirectDevice
	for attempt := 1; attempt <= incomingPeerAttempts; attempt++ {
		operation, err := statics.FromIdAsync(deviceID)
		if err != nil {
			log.Warnf("wifi direct: open incoming peer (attempt %d): %v", attempt, err)
		} else {
			peer, err = operation.Await()
			operation.Release()
			if err == nil {
				break
			}
			log.Warnf("wifi direct: establish incoming peer connection (attempt %d): %v", attempt, err)
		}
		if attempt == incomingPeerAttempts {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(incomingPeerRetryDelay):
		}
	}

	status, err := peer.ConnectionStatus()
	if err != nil {
		log.Warnf("wifi direct: read peer connection status: %v", err)
	} else {
		log.Infof("wifi direct: Windows peer connected (status=%s)", status)
	}
	logEndpointPairs(peer)

	peersMu.Lock()
	if *shuttingDown {
		peer.Release()
	} else {
		*peers = append(*peers, peer)
	}
	peersMu.Unlock()
}

// pairIncomingPeer runs the Wi-Fi Direct pairing ceremony for a peer that sent
// an incoming connection request. PBC arrives as ConfirmOnly; DisplayPin is
// accepted as well so a device that prefers a PIN is not rejected outright.
func pairIncomingPeer(pairing *enumeration.IDeviceInformationPairing) error {
	pairing2, err := winrt.QueryInterface[enumeration.IDeviceInformationPairing2](
		unsafe.Pointer(pairing),
		&enumeration.IID_IDeviceInformationPairing2,
	)
	if err != nil {
		return fmt.Errorf("pairing interface: %w", err)
	}
	defer pairing2.Release()

	custom, err := pairing2.Custom()
	if err != nil {
		return fmt.Errorf("custom pairing: %w", err)
	}
	defer custom.Release()

	handler, err := enumeration.NewTypedEventHandlerOfDeviceInformationCustomPairingAndDevicePairingRequestedEventArgs(
		func(_ *enumeration.IDeviceInformationCustomPairing, args *enumeration.IDevicePairingRequestedEventArgs) {
			kind, err := args.PairingKind()
			if err != nil {
				log.Warnf("wifi direct: read pairing kind: %v", err)
				return
			}
			log.Infof("wifi direct: pairing requested (kind=%s)", kind)
			switch kind {
			case enumeration.DevicePairingKindsConfirmOnly:
				if err := args.Accept(); err != nil {
					log.Warnf("wifi direct: accept pairing: %v", err)
				}
			case enumeration.DevicePairingKindsDisplayPin:
				pin, _ := args.Pin()
				log.Infof("wifi direct: pairing PIN %s", pin)
				if err := args.Accept(); err != nil {
					log.Warnf("wifi direct: accept pairing PIN: %v", err)
				}
			default:
				log.Warnf("wifi direct: unsupported pairing kind %s", kind)
			}
		},
	)
	if err != nil {
		return fmt.Errorf("pairing handler: %w", err)
	}
	defer handler.Close()

	token, err := custom.AddPairingRequested(handler)
	if err != nil {
		return fmt.Errorf("subscribe to pairing requests: %w", err)
	}
	defer custom.RemovePairingRequested(token)

	params, err := wifidirect.NewWiFiDirectConnectionParameters()
	if err != nil {
		return fmt.Errorf("pairing parameters: %w", err)
	}
	defer params.Release()
	// Windows owns the group; ask for the highest group-owner intent so the
	// negotiation with an Android client (intent 0) settles on Windows as GO.
	if err := params.SetGroupOwnerIntent(15); err != nil {
		return fmt.Errorf("group owner intent: %w", err)
	}
	if params2, err := params.AsWiFiDirectConnectionParameters2(); err != nil {
		return fmt.Errorf("pairing interface: %w", err)
	} else {
		if err := params2.SetPreferredPairingProcedure(wifidirect.WiFiDirectPairingProcedureGroupOwnerNegotiation); err != nil {
			params2.Release()
			return fmt.Errorf("pairing procedure: %w", err)
		}
		params2.Release()
	}

	settings, err := params.AsDevicePairingSettings()
	if err != nil {
		return fmt.Errorf("pairing settings: %w", err)
	}
	defer settings.Release()

	operation, err := custom.PairWithProtectionLevelAndSettingsAsync(
		enumeration.DevicePairingKindsConfirmOnly|enumeration.DevicePairingKindsDisplayPin,
		enumeration.DevicePairingProtectionLevelDefault,
		settings,
	)
	if err != nil {
		return fmt.Errorf("pair: %w", err)
	}
	result, err := operation.Await()
	operation.Release()
	if err != nil {
		return fmt.Errorf("pair await: %w", err)
	}
	status, err := result.Status()
	result.Release()
	if err != nil {
		return fmt.Errorf("pairing status: %w", err)
	}
	if status != enumeration.DevicePairingResultStatusPaired &&
		status != enumeration.DevicePairingResultStatusAlreadyPaired {
		return fmt.Errorf("pairing result %s", status)
	}
	return nil
}

// logEndpointPairs records the P2P addresses Windows assigned to the link.
func logEndpointPairs(peer *wifidirect.IWiFiDirectDevice) {
	pairs, err := peer.GetConnectionEndpointPairs()
	if err != nil {
		log.Warnf("wifi direct: read endpoint pairs: %v", err)
		return
	}
	defer pairs.Release()

	count, err := pairs.Size()
	if err != nil || count == 0 {
		log.Warnf("wifi direct: no endpoint pairs for connected peer")
		return
	}
	for i := uint32(0); i < count; i++ {
		pair, err := pairs.GetAt(i)
		if err != nil {
			continue
		}
		local, localErr := pair.LocalHostName()
		remote, remoteErr := pair.RemoteHostName()
		localName, remoteName := "", ""
		if localErr == nil {
			localName, _ = local.RawName()
			local.Release()
		}
		if remoteErr == nil {
			remoteName, _ = remote.RawName()
			remote.Release()
		}
		pair.Release()
		log.Infof("wifi direct: P2P endpoint local=%s remote=%s", localName, remoteName)
	}
}
