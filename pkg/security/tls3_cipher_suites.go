package security

type void struct{}

var TLS3CipherSuites = map[string]void{
	"TLS_AES_256_GCM_SHA384":       {},
	"TLS_CHACHA20_POLY1305_SHA256": {},
	"TLS_AES_128_GCM_SHA256":       {},
}
