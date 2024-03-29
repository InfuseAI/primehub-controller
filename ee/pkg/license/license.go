package license

import (
	"time"
)

type License struct {
	SignedLicense string
	Decoded       map[string]string
	Status        string
	Err           error
}

func NewLicense(signedLicense string, platformType string) (lic *License) {
	lic = &License{}
	lic.SignedLicense = signedLicense
	lic.Decoded = map[string]string{}

	_, err := Verify(signedLicense)
	if err != nil {
		lic.Err = err
		lic.Status = STATUS_INVALID
		return
	}
	lic.Decoded, err = Decode(signedLicense)
	if err != nil {
		lic.Err = err
		lic.Status = STATUS_INVALID
		return
	}

	if err := TypeCheck(lic.Decoded, platformType); err != nil {
		lic.Err = err
		lic.Status = STATUS_INVALID
		return
	}

	lic.Status = expiryStatus(lic.Decoded)
	return lic
}

func expiryStatus(decoded map[string]string) string {
	tExpiredAt, _ := time.Parse(TIME_LAYOUT, decoded["expired_at"])
	now := time.Now().UTC()
	status := STATUS_EXPIRED
	if now.Before(tExpiredAt) {
		status = STATUS_UNEXPIRED
	}
	return status
}
