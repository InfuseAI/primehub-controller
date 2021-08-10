package license

import (
	"time"
)

const (
	STATUS_EXPIRED   = "expired"
	STATUS_UNEXPIRED = "unexpired"
	STATUS_INVALID   = "invalid"
)

const (
	TIME_LAYOUT                = "2006-01-02T15:04:05Z"
	RESOURCE_NAME              = "primehub-license"
	DEFAULT_RESOURCE_NAMESPACE = "hub"
	SECRET_NAME                = "authoritative-secret"
	CHECK_EXPIRY_INTERVAL      = 1 * time.Hour

	/*
		licensed_to: "Default"
		started_at: "2020-01-01T00:00:00Z"
		expired_at: "2038-01-19T03:14:00Z"
		max_group: 0
		max_model_deploy: 1
	*/
	DEFAULT_SIGNED_LICENSE = "bGljZW5zZWRfdG86ICJEZWZhdWx0IgpzdGFydGVkX2F0OiAiMjAyMC0wMS0wMVQwMDowMDowMFoiCmV4cGlyZWRfYXQ6ICIyMDM4LTAxLTE5VDAzOjE0OjAwWiIKbWF4X2dyb3VwOiAwCm1heF9tb2RlbF9kZXBsb3k6IDEK.40b4d7a52ba6f3569e895aefdacd7eab259cd7c5c08e2d787126a4d54ebee7c63c8042f00c19cfb5e3e3570ae802512c9ddf43460554aaf24bc5eb8baeb979372dc55eaef707a0581b747bc5ac64534608c1dfb5593366f5897bfbe069a2f1c2a3babd562fa472fa605700e6d3ffc97a50416c695f973f8e392c2ef826a56b7e46f53ff8adc90e8a03281409f292b8d81e74b617e2fffa900bcad37c37060f45003abd3a7a24c6df415b65aa08ed09810b34b5912da9867550d078b121b3d0df5e7115f3d0b897e2e515f5db95ee9cefbbd2579a9430471b1cc79653e7e7bae5599ae9bebdc8440bec60276e8ad6af70c8cd056f44aaceaba91641a8dcae5ae6"

	PUBLIC_PEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAtIEgZLyg+tMgo08potRe
dgxs98M+p29J6WXOiOOF50b/JF7bvDutEmBLAvRN3DkA2Pwhu1llDVzyXnVZjFkO
br9/8NBr86NuHN9zT6mudF0hBViQlQa94lUp5I+CoZg3gB6238rkvXgG1S/TO9or
3QrqbT1ZS1uU8hIqg9aV//QEeUtOfXNoETwt34DhLqzjXnNHyW24ODpYkIzgGpbV
4RgitBExmrrAnnFMgfK7seWeu+H7s1ZME6aQ49DuxiFHXTAcG7zRO0m4PJ1yZTTF
3abgThdEfY3BsFeP+E0zLSIPL2vvqLbEReWlZ4JqG59G91w+eAFZtpq4Mw4s3ihU
SQIDAQAB
-----END PUBLIC KEY-----`
)
