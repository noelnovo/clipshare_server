package certs_test

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"clipshare/src/internal/certs"
)

func TestInitCreatesCA(t *testing.T) {
	dir := t.TempDir()
	if err := certs.Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, certs.CaCertFile)); err != nil {
		t.Errorf("CA cert missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, certs.CaKeyFile)); err != nil {
		t.Errorf("CA key missing: %v", err)
	}
}

func TestInitRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := certs.Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := certs.Init(dir); err == nil {
		t.Error("expected error when CA already exists")
	}
}

func TestIssueServerCert(t *testing.T) {
	dir := t.TempDir()
	if err := certs.Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := certs.Issue(dir, "server", "server", []string{"192.168.1.5"}); err != nil {
		t.Fatalf("Issue server failed: %v", err)
	}

	cert, err := certs.LoadKeyPair(filepath.Join(dir, "server.pem"), filepath.Join(dir, "server.key"))
	if err != nil {
		t.Fatalf("LoadKeyPair failed: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("no certificates in key pair")
	}

	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if parsed.Subject.CommonName != "server" {
		t.Errorf("CN: got %q, want server", parsed.Subject.CommonName)
	}
	if len(parsed.IPAddresses) != 1 || parsed.IPAddresses[0].String() != "192.168.1.5" {
		t.Errorf("SAN IPs: got %v, want [192.168.1.5]", parsed.IPAddresses)
	}
}

func TestIssueClientCert(t *testing.T) {
	dir := t.TempDir()
	if err := certs.Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := certs.Issue(dir, "phone", "client", nil); err != nil {
		t.Fatalf("Issue client failed: %v", err)
	}

	cert, err := certs.LoadKeyPair(filepath.Join(dir, "phone-client.pem"), filepath.Join(dir, "phone-client.key"))
	if err != nil {
		t.Fatalf("LoadKeyPair failed: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if parsed.Subject.CommonName != "phone" {
		t.Errorf("CN: got %q, want phone", parsed.Subject.CommonName)
	}
}

func TestLoadPool(t *testing.T) {
	dir := t.TempDir()
	if err := certs.Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	pool, err := certs.LoadPool(filepath.Join(dir, certs.CaCertFile))
	if err != nil {
		t.Fatalf("LoadPool failed: %v", err)
	}
	if pool == nil {
		t.Error("expected non-nil pool")
	}
}

func TestLoadPoolMissingFile(t *testing.T) {
	_, err := certs.LoadPool(filepath.Join(t.TempDir(), "missing.pem"))
	if err == nil {
		t.Error("expected error for missing CA file")
	}
}

func TestTrust(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	if err := certs.Init(dirA); err != nil {
		t.Fatal(err)
	}
	if err := certs.Init(dirB); err != nil {
		t.Fatal(err)
	}
	caB := filepath.Join(dirB, certs.CaCertFile)
	if err := certs.Trust(dirA, caB); err != nil {
		t.Fatalf("Trust failed: %v", err)
	}
	// ca.pem in dirA should now contain two certificates.
	data, _ := os.ReadFile(filepath.Join(dirA, certs.CaCertFile))
	n := 0
	rest := data
	for {
		block, r := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = r
		if block.Type == "CERTIFICATE" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("expected 2 certs after trust, got %d", n)
	}
	// Trusting again is idempotent.
	if err := certs.Trust(dirA, caB); err != nil {
		t.Fatalf("second Trust failed: %v", err)
	}
	data2, _ := os.ReadFile(filepath.Join(dirA, certs.CaCertFile))
	n2 := 0
	rest2 := data2
	for {
		block, r := pem.Decode(rest2)
		if block == nil {
			break
		}
		rest2 = r
		if block.Type == "CERTIFICATE" {
			n2++
		}
	}
	if n2 != 2 {
		t.Errorf("expected still 2 certs after re-trust, got %d", n2)
	}
}
