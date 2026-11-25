# PTB CLI

```text
██████    ████████   ██████
██   ██      ██      ██   ██
██████       ██      ██████
██           ██      ██   ██
██           ██      ██   ██
██           ██      ██████
```

`ptb` membaca data [PaperToBillion](https://papertobillion.com) dari terminal, skrip, dan agen AI: broker summary, Broker Screener, Broker Stalker, foreign flow, picks, Net Flow, indeks, profil saham, aksi korporasi, sampai Order Flow dan Heatmap.

CLI ini klien publik dari server PTB. Semua data dan perhitungan ada di server; membership, saldo token, dan tarifnya **sama** dengan web dan menu **Integrasi** (MCP).

## Instalasi

| Sistem | Perintah |
|---|---|
| macOS / Linux (Homebrew) | `brew install ealfarozi/tap/ptb` |
| Windows (WinGet) | `winget install ealfarozi.ptb` |
| Manual | unduh dari [Releases](https://github.com/ealfarozi/ptb-cli/releases), cocokkan `checksums.txt` |

Verifikasi tanda tangan rilis (opsional, butuh [cosign](https://docs.sigstore.dev/)):

```bash
cosign verify-blob --certificate checksums.txt.pem --signature checksums.txt.sig \
  --certificate-identity-regexp 'https://github.com/ealfarozi/ptb-cli/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
```

## Masuk

```bash
ptb login
```

Browser membuka halaman login PTB. Login seperti biasa (Discord, Google, atau passkey), lalu tekan **Setujui**. Sesi disimpan di keychain sistem. CLI tampil di **Integrasi → Aplikasi terhubung** dan bisa dicabut dari sana kapan saja.

**Server, CI, atau komputer tanpa browser:** buat API key di **Integrasi → Keamanan → Buat API key**, lalu:

```bash
export PTB_API_KEY=ptb_xxxxxxxx
```

Jangan tulis API key di argumen perintah atau file config.

```bash
ptb whoami             # akun dan sumber login
ptb account status     # membership dan akses
ptb tokens balance     # saldo token
ptb logout             # hapus sesi lokal + cabut di server
```

## Perintah data

Gratis untuk member Prime aktif:

| Perintah | Isi |
|---|---|
| `ptb screener --from 2026-11-01 --to 2026-11-24` | Broker Screener |
| `ptb broker summary BBCA --from … --to …` | Broker summary |
| `ptb broker net-position BBCA --from … --to …` | Posisi bersih broker per hari |
| `ptb broker inventory BBCA,TLKM --from … --to …` | Inventory broker |
| `ptb broker activity AK --from … --to …` | Saham yang paling dibeli/dijual satu broker |
| `ptb broker stalker YP --from … --to …` | Broker Stalker |
| `ptb broker foreign-list` | Daftar broker asing |
| `ptb foreign-flow --from … --to …` | Foreign flow seluruh pasar |
| `ptb net-flow --sort haka` | Net Flow HAKA/HAKI |
| `ptb picks today` / `ptb picks history` | Picks |
| `ptb most-active`, `ptb bubbles`, `ptb index-ptb` | Data pasar |
| `ptb index IHSG`, `ptb index-flow` | Indeks dan aliran dana IHSG |
| `ptb premarket` | Pre-opening (IEP) |
| `ptb stock ADRO --full` | Profil saham |
| `ptb corporate-action BBCA` | Aksi korporasi |
| `ptb scripless --mode top` | Scripless |
| `ptb watchlist` | Watchlist kamu |
| `ptb extreme-results` | Kalender extreme results |

Memotong token untuk **sesi hari ini** (sesi yang sudah selesai gratis, riwayat 180 hari):

| Perintah | Tarif |
|---|---|
| `ptb orderflow BBCA` | 1 token per 1.000 transaksi yang belum dibayar, minimal 1 token (sama dengan Play di web) |
| `ptb volume-profile`, `ptb big-trades`, `ptb done-trades`, `ptb auction-hub`, `ptb book-events` | sama dengan Order Flow; transaksi yang sudah dibayar tidak ditagih lagi |
| `ptb heatmap BBCA --minutes 30` | 1 token per menit per saham |

Data live tersedia di hari bursa, 08:55–16:16 WIB. Semua tool dan tarifnya: `ptb tools list`.

## Token: konfirmasi, batas, dan jalan tanpa tanya

Sebelum perintah berbayar jalan, CLI meminta perkiraan biaya ke server lalu bertanya:

```text
$ ptb orderflow BBCA
ptb_get_orderflow BBCA 2026-11-25
Perkiraan biaya: 3 token · saldo 120 token
Lanjutkan? [y/N] y
…
Terpotong: 3 token · sisa 117 token
```

- `--yes` (`-y`) langsung lanjut tanpa pertanyaan.
- `--max-tokens N` adalah batas yang **ditegakkan server**: kalau perkiraannya lebih dari N, permintaan ditolak (`BUDGET_EXCEEDED`, kode keluar 5) sebelum token dipotong. Tanpa `--max-tokens`, batasnya angka perkiraan yang sudah tampil, jadi biaya tidak bisa melebihi yang kamu setujui.
- Kalau panggilan gagal setelah token terpotong, token **dikembalikan otomatis** (tercatat di riwayat).
- Jaringan putus lalu CLI mengulang? Setiap panggilan berbayar membawa `Idempotency-Key`, jadi tidak ada potong dobel.
- Perkiraan tanpa memotong: `ptb billing estimate orderflow BBCA`. Riwayat: `ptb billing history`.

Contoh skrip tanpa interaksi:

```bash
ptb orderflow BBCA --yes --max-tokens 10 --output json > bbca.json
```

## Output untuk skrip dan agen AI

`--output json|csv|ndjson` menghasilkan stdout yang bersih tanpa warna, spinner, atau banner; pesan ada di stderr. Panduan untuk agen: `ptb agent instructions`.

| Kode keluar | Arti |
|---|---|
| 0 | berhasil |
| 2 | argumen salah |
| 3 | perlu login / sesi kedaluwarsa |
| 4 | tidak punya akses (membership tidak aktif) |
| 5 | token tidak cukup atau melebihi `--max-tokens` |
| 6 | pasar tutup, riwayat lewat batas, atau data belum ada |
| 7 | rate limit (120 permintaan/menit, 4 sekaligus) |
| 8 | jaringan atau server |
| 10 | dibatalkan |

## Tampilan

```bash
ptb config set ui.theme light      # atau dark
ptb config set ui.animations false
ptb config set ui.banner false
ptb --no-color …    ptb --plain …
ptb completion zsh > "${fpath[1]}/_ptb"   # juga bash, fish, powershell
```

`NO_COLOR` dihormati. Banner dan animasi hanya muncul di terminal interaktif.

## Lainnya

- `ptb doctor` memeriksa versi, login, dan koneksi tanpa menampilkan rahasia.
- `ptb update` / `ptb update check` menampilkan versi terbaru dan perintah pembaruannya.
- Profil lain: `--profile kantor` atau `PTB_PROFILE=kantor`.

## Pengembangan

```bash
go test ./...
go build -o ptb .
PTB_API_URL=http://localhost:8080/api/v2/mcp PTB_AUTH_URL=http://localhost:8080 ./ptb account status
```

Rilis: push tag `vX.Y.Z` → GitHub Actions menjalankan GoReleaser (biner multi-OS, checksum bertanda tangan Sigstore, SBOM, formula Homebrew, manifest WinGet).

Lisensi Apache-2.0. Data pasar tetap milik PTB dan penyedianya; CLI ini tidak menyertakan data.
