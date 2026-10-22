// Generates a short-lived localhost certificate for the isolated R11 check only.
// Run with Go on the host, or in the pinned Go build image; never use for a pilot.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: local-cert DIRECTORY")
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	cert := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "R11 localhost fixture"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(24 * time.Hour),
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		return err
	}
	// Refuse to overwrite any pre-existing certificate or private key.
	for _, output := range []struct {
		name  string
		block *pem.Block
		mode  os.FileMode
	}{
		{"privkey.pem", &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}, 0600},
		{"fullchain.pem", &pem.Block{Type: "CERTIFICATE", Bytes: der}, 0644},
	} {
		f, err := os.OpenFile(filepath.Join(dir, output.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, output.mode)
		if err != nil {
			return err
		}
		err = pem.Encode(f, output.block)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
