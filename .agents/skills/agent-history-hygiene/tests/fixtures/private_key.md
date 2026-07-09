# private key fixture

Contains a fake PEM-format private key block. `redact_private_keys`
must replace the block with `[REDACTED PRIVATE KEY BLOCK]`, and the
literal string `PRIVATE KEY` elsewhere must become `PRIV***KEY`.

-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEAFAKE_KEY_MATERIAL_FOR_TESTING_ONLY_DO_NOT_USE
xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
-----END RSA PRIVATE KEY-----

And a bare mention of PRIVATE KEY outside any block — redactor should
still catch this.
