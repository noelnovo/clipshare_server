// Package cert implements the `clipshare cert` subcommand for managing the
// private mTLS CA and per-device certificates.
package cert

import (
	"fmt"

	"clipshare/src/internal/certs"
)

// Command dispatches the cert subcommands.
type Command struct{}

func (Command) Run(args []string) error {
	if len(args) < 1 {
return fmt.Errorf(`usage: clipshare cert <init|issue|export|qr|list>

  clipshare cert init                                  create the private CA
  clipshare cert issue --name X --type server|client   issue a certificate
                       [--ip 192.168.1.10,192.168.1.11]  (SAN IPs, servers only)
  clipshare cert export --name X --type server|client  write a PKCS#12 bundle
                       [--out path.p12]
  clipshare cert qr --name X --type server|client      print a QR the ClipShare
                       [--out path.png]                  app can scan to import
                                                          this device identity
                                                          and trust this server
  clipshare cert trust <path-to-foreign-ca.pem>        add another daemon's CA
                                                          to this daemon's
                                                          trust store (multi-CA
                                                          TLS, no re-pairing)
  clipshare cert list                                  show issued certificates`)
	}
	dir, err := certDir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "init":
		return certs.Init(dir)
	case "issue":
		return runIssue(dir, args[1:])
	case "export":
		return runExport(dir, args[1:])
	case "qr":
		return runQr(dir, args[1:])
	case "list":
		return runList(dir)
	case "trust":
		if len(args) < 2 {
			return fmt.Errorf("usage: clipshare cert trust <path-to-foreign-ca.pem>")
		}
		return runTrust(dir, args[1])
	default:
		return fmt.Errorf("unknown cert subcommand %q", args[0])
	}
}
