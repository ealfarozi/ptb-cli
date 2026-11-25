# Keamanan

Laporkan celah keamanan secara privat ke admin PTB lewat Discord PaperToBillion, atau buka
[GitHub Security Advisory](https://github.com/ealfarozi/ptb-cli/security/advisories/new).
Jangan kirim API key, token, atau data akun di laporan publik.

- Sesi disimpan di keychain sistem (fallback berkas 0600 bila keychain tidak ada).
- Login memakai OAuth 2.1 + PKCE S256 dengan redirect loopback; kata sandi tidak pernah diketik di terminal.
- Rilis ditandatangani (Sigstore keyless) dan disertai checksum serta SBOM.
