# Changelog

## v0.1.4 — 2026-11-27
- `ptb guide [fitur]`: panduan Help Center sesuai akses akun (sama dengan tool MCP ptb_get_guide).
- `ptb journal read|write`: Jurnal trading (khusus admin dan akun internal, dibatasi server).
- Judul kolom tabel tanpa awalan angka.

## v0.1.3 — 2026-11-25
- Distribusi WinGet: manifest `ealfarozi.ptb` diajukan otomatis ke microsoft/winget-pkgs.

## v0.1.2 — 2026-11-25
- `ptb update` mengenali pemasangan lewat Scoop dan Docker.
- Formula Homebrew dan manifest Scoop kini diperbarui otomatis oleh rilis.

## v0.1.1 — 2026-11-25
- Distribusi baru: Scoop (`ealfarozi/scoop-bucket`) dan image Docker `ghcr.io/ealfarozi/ptb` (linux/amd64, linux/arm64).

## v0.1.0 — 2026-11-25
- Rilis pertama: login browser (OAuth PKCE) dan PTB_API_KEY, akun/saldo/riwayat, 30 perintah data
  (semua tool member MCP), konfirmasi biaya dengan `--yes` dan `--max-tokens` yang ditegakkan
  server, Idempotency-Key, refund otomatis, output table/json/csv/ndjson, banner PTB, config,
  doctor, update check, completion.
