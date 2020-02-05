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
	DEFAULT_RESOURCE_NAMESPACE = "primehub"
	SECRET_NAME                = "authoritative-secret"
	CHECK_EXPIRY_INTERVAL      = 1 * time.Hour

	/*
		licensed_to: "Default"
		started_at: "2020-01-01T00:00:00Z"
		expired_at: "2038-01-19T03:14:00Z"
		max_group: 0
	*/
	DEFAULT_SIGNED_LICENSE = "bGljZW5zZWRfdG86ICJEZWZhdWx0IgpzdGFydGVkX2F0OiAiMjAyMC0wMS0wMVQwMDowMDowMFoiCmV4cGlyZWRfYXQ6ICIyMDM4LTAxLTE5VDAzOjE0OjAwWiIKbWF4X2dyb3VwOiAwCg==.3d708e53dbb6b81672410b0c04a26d59ca19d20edc1d49e7278483454336c3f71de96d8d8b6ab6ed826db68b00b6bc4e566318e05dad6515ddcfdbce912fff128b5994d17bb542da86edc7efb48bfd4a4dfce8b3f6f8c4ed93835738b2cead2410a91252f37123bf3466a89fc9e51d0856ac4dbefdbf35df1d0d521d2ee2d3437ae27a01f06945f626306a99f973be20522307d025d2594f9b80cfdb181fc160671b90e2af73315f77f7d4330bd2a859469f13f41f9d77202aa341032a3d731d7ce3af4424b5cbea21d5ed3502912187f934cbf9d59f2c70e567d09e999f4dddf5b58e0d9919d4d3181953d74906eb444be845ad4fff1de0d34678937553bd0a"

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