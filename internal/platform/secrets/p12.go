package secrets

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// P12ToPEM decodes a PKCS#12 bundle into PEM-encoded certificate and private key.
func P12ToPEM(p12 []byte, password string) (certPEM, keyPEM []byte, err error) {
	key, cert, err := pkcs12.Decode(p12, password)
	if err != nil {
		return nil, nil, fmt.Errorf("secrets: decode p12: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("secrets: marshal key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	return certPEM, keyPEM, nil
}
