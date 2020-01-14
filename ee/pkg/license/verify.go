package license

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"
)

func Verify(signedLicense string) (ok bool, err error) {
	splits := strings.Split(signedLicense, ".")
	if len(splits) != 2 {
		return false, errors.New("malformed signed license key")
	}

	message, sig := splits[0], splits[1]
	hashed := sha256.Sum256([]byte(message))
	block, _ := pem.Decode([]byte(PUBLIC_PEM))
	pub, _ := x509.ParsePKIXPublicKey(block.Bytes)
	pubKey, _ := pub.(*rsa.PublicKey)

	signature, err := hex.DecodeString(sig)
	if err != nil {
		return false, errors.New("decode license signature failed: " + err.Error())
	}

	err = rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hashed[:], signature)
	if err != nil {
		return false, errors.New("license verification failed: " + err.Error())
	} else {
		return true, nil
	}
}
