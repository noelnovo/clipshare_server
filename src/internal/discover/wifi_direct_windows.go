//go:build windows && amd64

package discover

import (
	"context"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
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
	if !cfg.WiFiDirect.Enabled {
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
			defer request.Release()

			information, err := request.DeviceInformation()
			if err != nil {
				log.Warnf("wifi direct: get connecting peer information: %v", err)
				return
			}
			deviceID, err := information.Id()
			information.Release()
			if err != nil {
				log.Warnf("wifi direct: get connecting peer ID: %v", err)
				return
			}

			go func() {
				if err := winrt.Initialize(); err != nil {
					log.Warnf("wifi direct: initialize runtime for incoming peer: %v", err)
					return
				}
				statics, err := wifidirect.WiFiDirectDeviceStatics2()
				if err != nil {
					log.Warnf("wifi direct: open Windows peer connection: %v", err)
					return
				}
				defer statics.Release()
				// Windows is the group owner here, so request GO intent 15 with
				// GroupOwnerNegotiation. FromIdAsync without parameters returns
				// E_ILLEGAL_METHOD_CALL (0x8000000E) for these requests.
				params, err := wifidirect.NewWiFiDirectConnectionParameters()
				if err != nil {
					log.Warnf("wifi direct: incoming connection parameters: %v", err)
					return
				}
				if err := params.SetGroupOwnerIntent(15); err != nil {
					log.Warnf("wifi direct: incoming group owner intent: %v", err)
					return
				}
				if params2, err := params.AsWiFiDirectConnectionParameters2(); err != nil {
					log.Warnf("wifi direct: incoming pairing interface: %v", err)
					return
				} else if err := params2.SetPreferredPairingProcedure(wifidirect.WiFiDirectPairingProcedureGroupOwnerNegotiation); err != nil {
					log.Warnf("wifi direct: incoming pairing procedure: %v", err)
					return
				}
				// The connection request can arrive before WinRT has registered
				// the peer device. Retry briefly instead of failing the pairing.
				var peer *wifidirect.IWiFiDirectDevice
				for attempt := 1; attempt <= incomingPeerAttempts; attempt++ {
					operation, err := statics.FromIdAsync(deviceID, &params.IWiFiDirectConnectionParameters)
					if err != nil {
						log.Warnf("wifi direct: accept incoming peer (attempt %d): %v", attempt, err)
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
				peersMu.Lock()
				if shuttingDown {
					peer.Release()
				} else {
					peers = append(peers, peer)
				}
				peersMu.Unlock()
			}()
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
