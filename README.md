# jasmr-dl

Download audio from [japaneseasmr.com](https://japaneseasmr.com) posts, tagged and with cover art.

- Downloads every part of a post in parallel
- Embeds cover art and writes title, artist, circle, RJ code, date and track numbers
- Cuts a chaptered stream into one file per chapter
- Saves the post's image gallery alongside the audio
- Handles posts that serve only the site's stream
- Names files and directories from a template
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

Files land under the post's year and RJ code:

```
2024/RJ123456/RJ123456_1.mp3
2024/RJ123456/RJ123456_2.mp3
2024/RJ123456/jacket.jpg
2024/RJ123456/images/01.jpg
```

A post that serves a chaptered stream is cut into its chapters instead:

```
2024/RJ123456/01_はじめに.m4a
2024/RJ123456/02_耳かき.m4a
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

## Output template

`-o` holds the path and the filename together. Its last segment always names the
file, anything before it names directories, and the path may be relative or
absolute.

```
jasmr-dl https://japaneseasmr.com/12345/ -o "{circle}/{rjcode}/{number}. {chapter}.{ext}"
jasmr-dl https://japaneseasmr.com/12345/ -o "C:/Audio/{year}/{title}/{rjcode}_{number}.{ext}"
jasmr-dl https://japaneseasmr.com/12345/ -P ./out
jasmr-dl https://japaneseasmr.com/12345/ -P ./out -o "{rjcode}/{number}.{ext}"
```

| Field | Names |
| --- | --- |
| `{title}` | Post title |
| `{rjcode}` | DLsite code |
| `{circle}` | Circle |
| `{artist}` | Voice actor |
| `{date}` `{year}` `{month}` `{day}` | Post date |
| `{number}` | File number |
| `{chapter}` | Chapter title |
| `{track}` `{tracktotal}` | Track number |
| `{ext}` | File extension |

A field the post does not carry writes `Unknown`. `{number}`, `{chapter}`,
`{track}`, `{tracktotal}` and `{ext}` belong to the filename, not a directory.

The default is `{year}/{rjcode}/{rjcode}_{number}.{ext}`, except where a stream
is cut into chapters, which defaults to `{year}/{rjcode}/{number}_{chapter}.{ext}`.
Passing `-o` replaces both.

Cover art and the gallery follow the audio: `jacket.jpg` beside it, the rest
under `images/`.

## Chapters

A post serves either separate files, or one stream with a track list beside it.
The stream is cut into one file per chapter.

| | |
| --- | --- |
| default | One file per chapter |
| `-S` | One file, the track list embedded as chapters |
| `-H` | One file, no chapter metadata at all |

## Options

| Flag | Default | Description |
| --- | --- | --- |
| `-o, --output` | `{year}/{rjcode}/{rjcode}_{number}.{ext}` | Template naming each file and the directories above it |
| `-P, --paths` | | Directory everything is written under |
| `-N, --concurrency` | `3` | Files downloaded at once |
| `-j, --connections` | `32` | Ranged requests in flight; this is what sets speed (max 128) |
| `-R, --retries` | `4` | Retry attempts per ranged request |
| `-c, --cookies` | | Path to a `cookies.txt` export, saved for later runs |
| `--use-browser` | | Path to a browser executable that clears a Cloudflare challenge |
| `--show-browser` | `false` | Show that browser instead of running it headless |
| `-C, --no-cover` | `false` | Do not embed cover art |
| `-I, --no-images` | `false` | Do not save the rest of the post's gallery |
| `-H, --no-chapters` | `false` | Do not use the track list: no chapters, no split |
| `-S, --no-split` | `false` | Do not cut a chaptered stream into one file per chapter |
| `-T, --no-tags` | `false` | Do not write title, artist or album metadata |
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
