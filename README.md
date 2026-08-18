# jasmr-dl

Download audio from [japaneseasmr.com](https://japaneseasmr.com) posts, tagged and with cover art.

- Downloads every part of a post in parallel
- Embeds cover art and writes title, artist, circle, RJ code and date
- Embeds the track list as chapters when one file holds the whole work
- Saves the post's image gallery alongside the audio
- Handles posts that serve only the site's stream
- Clears Cloudflare challenges in a browser and reuses the clearance

## Install

Download a binary for your platform from [Releases](https://github.com/EagleStelle/jasmr-dl/releases).

Or with Go:

```
go install github.com/EagleStelle/jasmr-dl@latest
```

## Usage

```
jasmr-dl https://japaneseasmr.com/12345/
```

Files land in a directory named after the post. Override with `-o`:

```
jasmr-dl https://japaneseasmr.com/12345/ -o ./out
```

Go faster with more ranged requests in flight, and more files at once:

```
jasmr-dl https://japaneseasmr.com/12345/ -j 64 -N 8
```

Audio only:

```
jasmr-dl https://japaneseasmr.com/12345/ -C -I -T
```

`Ctrl+C` cancels. Exit status is non-zero only when every file fails.

## Options

| Flag | Default | Description |
| --- | --- | --- |
| `-o, --output` | post title | Download directory |
| `-N, --concurrency` | `3` | Files downloaded at once |
| `-j, --connections` | `32` | Ranged requests in flight; this is what sets speed (max 128) |
| `-R, --retries` | `4` | Retry attempts per ranged request |
| `-c, --cookies` | | Path to a `cookies.txt` export, saved for later runs |
| `--use-browser` | | Browser executable used to clear a Cloudflare challenge |
| `--show-browser` | `false` | Show that browser instead of running it headless |
| `-C, --no-cover` | `false` | Do not embed cover art |
| `-I, --no-images` | `false` | Do not save the rest of the gallery |
| `-H, --no-chapters` | `false` | Do not embed the track list as chapters |
| `-T, --no-tags` | `false` | Do not write metadata |
| `-v, --verbose` | `false` | Debug logging on stderr |

## Requirements

`ffmpeg` and `ffprobe` are required for cover art, tags, and posts that serve only the site's stream. Put both on `PATH`, or beside the `jasmr-dl` binary.

Without them, use `-C -T -H` to download the audio untouched. Stream-only posts will not work.

## Cloudflare

When a challenge appears, jasmr-dl opens a browser to clear it, writes `cookies.txt` beside the binary, and reuses it on later runs. Point `--use-browser` at an executable if none is found, and pass `--show-browser` to watch it.

If that fails, open the post in your own browser, export its cookies as `cookies.txt`, and leave the file beside the binary. Pass a path with `-c` once and it is copied into place.

## Build

```
make build
make test
```
