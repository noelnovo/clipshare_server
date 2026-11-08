package certs

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// Trust appends the CA certificate(s) found in foreignPath to the local
// ca.pem, so this daemon also trusts peers/clients issued by that foreign
// CA. The local CA is kept as the first certificate (it remains the issuing
// CA for this machine's own certs); foreign CAs are appended only if they are
// not already present (compared by certificate DER). This lets two machines
// keep their own CAs (so existing Android clients stay paired) while still
// trusting each other for mutual TLS -- no shared CA file has to be copied by
// hand and no client has to be re-paired.
func Trust(dir, foreignPath string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	localPath := filepath.Join(dir, CaCertFile)
	localData, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local %s: %w (run 'clipshare cert init' first)", CaCertFile, err)
	}
	localCerts, err := parseAllCerts(localData)
	if err != nil {
		return fmt.Errorf("parse local %s: %w", CaCertFile, err)
	}
	if len(localCerts) == 0 {
		return fmt.Errorf("no CA found in %s (run 'clipshare cert init' first)", CaCertFile)
	}

	foreignData, err := os.ReadFile(foreignPath)
	if err != nil {
		return fmt.Errorf("read foreign CA %s: %w", foreignPath, err)
	}
	foreignCerts, err := parseAllCerts(foreignData)
	if err != nil {
		return fmt.Errorf("parse foreign CA %s: %w", foreignPath, err)
	}
	if len(foreignCerts) == 0 {
		return fmt.Errorf("no certificates found in %s", foreignPath)
	}

	// Index existing certs by DER for dedup.
	existing := make(map[string]bool)
	for _, c := range localCerts {
		existing[string(c.Raw)] = true
	}

	var appended int
	var buf bytes.Buffer
	buf.Write(localData)
	// Ensure localData ends with a newline before appending.
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		buf.WriteByte('\n')
	}
	for _, c := range foreignCerts {
		if existing[string(c.Raw)] {
			continue
		}
		// Only trust CA certificates.
		if !c.IsCA {
			return fmt.Errorf("certificate CN=%q in %s is not a CA; refusing to trust a leaf", c.Subject.CommonName, foreignPath)
		}
		block := &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}
		if err := pem.Encode(&buf, block); err != nil {
			return err
		}
		existing[string(c.Raw)] = true
		appended++
	}

	if appended == 0 {
		fmt.Printf("all certificates in %s already trusted; nothing changed\n", foreignPath)
		return nil
	}
	if err := os.WriteFile(localPath, buf.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("trusted %d new CA(s) from %s (total %d CA(s) in %s)\n",
		appended, foreignPath, len(localCerts)+appended, localPath)
	return nil
}

// parseAllCerts returns every CERTIFICATE block in data.
func parseAllCerts(data []byte) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	rest := data
	for {
		block, r := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = r
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		out = append(out, cert)
	}
	return out, nil
}
