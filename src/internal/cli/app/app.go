package app

import (
	"context"
	"errors"

	"clipshare/src/internal/api"
	"clipshare/src/internal/clip"
	"clipshare/src/internal/config"
	"clipshare/src/internal/consts"
	"clipshare/src/internal/discover"
	"clipshare/src/internal/log"
	"clipshare/src/internal/websocket"
)

// App wires together the clipboard, WebSocket server, localhost API, peer
// connections, and LAN discovery. It is the runtime heart of the daemon.
type App struct {
	cfg       *config.Config
	clipboard clip.Interface
	server    *websocket.Server
	api       *api.Server
	watcher   *clip.Watcher
	version   string
}

// startWiFiDirectClient connects this PC to a phone group owner when
// wifi_direct.role is "client". Each new owner address gets one mTLS
// WebSocket connection, which reconnects on its own while the daemon runs.
func (a *App) startWiFiDirectClient(ctx context.Context) {
	if !a.cfg.WiFiDirect.Enabled || a.cfg.WiFiDirect.Role != config.WiFiDirectRoleClient {
		return
	}
	if !a.cfg.TLS.Enabled {
		log.Errorf("wifi direct client: [tls].enabled = false; Wi-Fi Direct requires mutual TLS")
		return
	}
	tlsCfg, err := websocket.GroupOwnerTLSConfig(a.cfg)
	if err != nil {
		log.Errorf("wifi direct client: %v", err)
		return
	}
	var lastOwner string
	discover.RunWiFiDirectClient(ctx, a.cfg, func(ownerIP string) {
		if ownerIP == lastOwner {
			return
		}
		lastOwner = ownerIP
		log.Infof("wifi direct client: dialing group owner wss://%s:%d", ownerIP, a.cfg.Server.Port)
		go a.server.RunGroupOwner(ctx, ownerIP, tlsCfg, a.version, a.writeRemote)
	})
}

// RunOptions controls optional daemon behavior.
type RunOptions struct {
	NoWatch bool
}

// New creates an App from a loaded config. The clipboard backend is always
// initialized so remote content can be written locally.
func New(cfg *config.Config, version string) (*App, error) {
	clipboard, err := clip.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	srv := websocket.New(cfg, version, func(content clip.Content, from string) {
		// placeholder; set by Run after watcher exists
	})

	apiSrv := api.NewServer(cfg, srv, srv, version)

	return &App{
		cfg:       cfg,
		clipboard: clipboard,
		server:    srv,
		api:       apiSrv,
		version:   version,
	}, nil
}

// Run starts all background services and blocks until ctx is cancelled.
func (a *App) Run(ctx context.Context, opts RunOptions) error {
	defer a.clipboard.Close()

	// Wire the remote-write callback after the watcher exists so we can use
	// LocalWrite when watching is enabled (avoiding rebroadcast loops).
	if !opts.NoWatch {
		var watcherOpts []clip.WatcherOption
		notifyCh, stopNotify, err := clip.StartClipboardNotifier(ctx)
		defer stopNotify()
		switch {
		case err == nil:
			watcherOpts = append(watcherOpts, clip.WithNotifier(notifyCh))
			log.Infof("clipboard watching: event-driven")
		case errors.Is(err, clip.ErrNotifierUnavailable):
			log.Debugf("clipboard watching: polling every %s (%v)", a.cfg.WatchInterval(), err)
		default:
			log.Warnf("clipboard change events unavailable (%v); falling back to %s polling",
				err, a.cfg.WatchInterval())
		}
		a.watcher = clip.NewWatcher(a.clipboard, a.cfg.WatchInterval(), func(content clip.Content) {
			log.Debugf("local clipboard changed")
			a.server.BroadcastLocal(content, a.cfg.DeviceName)
		}, watcherOpts...)
		a.server.SetOnRemoteClip(a.writeRemote)
		go a.watcher.Run(ctx)
	} else {
		a.server.SetOnRemoteClip(func(content clip.Content, from string) {
			a.clipboard.Write(content)
		})
		log.Infof("clipboard watching disabled; only receiving")
	}

	go func() {
		if err := a.server.ListenAndServe(ctx); err != nil {
			log.Errorf("ws server: %v", err)
		}
	}()
	go a.server.RunPeers(ctx, a.version, a.writeRemote)
	a.startWiFiDirectClient(ctx)

	if err := discover.Start(ctx, a.cfg); err != nil {
		log.Errorf("discovery: %v", err)
	}

	go func() {
		if err := a.api.Listen(ctx); err != nil {
			log.Errorf("api server: %v", err)
		}
	}()

	proto := consts.SchemeWS
	if a.cfg.TLS.Enabled {
		proto = consts.SchemeWSS
	}
	log.Infof("clipshare %s running as %q (%s %s:%d, api %s:%d)",
		a.version, a.cfg.DeviceName, proto,
		a.cfg.Server.Bind, a.cfg.Server.Port,
		a.cfg.API.Bind, a.cfg.API.Port)

	<-ctx.Done()
	log.Infof("shutting down")
	return nil
}

// writeRemote writes remote clipboard content locally, using the watcher when
// available so the change is not rebroadcast as a local change.
func (a *App) writeRemote(content clip.Content, from string) {
	if content.Kind == clip.KindImage {
		if len(content.Image) == 0 {
			return
		}
	} else if content.Text == "" {
		return
	}
	log.Debugf("remote clipboard from %q -> writing to local", from)
	if a.watcher != nil {
		a.watcher.LocalWrite(content)
	} else {
		a.clipboard.Write(content)
	}
}
