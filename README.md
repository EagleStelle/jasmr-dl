# jasmr-dl

Download audio from [japaneseasmr.com](https://japaneseasmr.com) posts, tagged and with cover art.

- Downloads several posts, and every part of each one, in parallel
- Reads a list of URLs from a file
- Embeds cover art and writes title, artist, circle, RJ code, date and track numbers
- Records the post URL and its tags in the comment field
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

Files land under the post's RJ code:

```
RJ123456/RJ123456_1.mp3
RJ123456/RJ123456_2.mp3
RJ123456/cover.jpg
RJ123456/images/01.jpg
```

A post that serves a chaptered stream is cut into its chapters instead:

```
RJ123456/01_はじめに.m4a
RJ123456/02_耳かき.m4a
```

Go faster with more ranged requests in flight, and more files at once:

```
jasmr-dl https://japaneseasmr.com/12345/ -j 64 -N 8
```

Audio only:

```
jasmr-dl https://japaneseasmr.com/12345/ -M -C -I
```

`Ctrl+C` cancels. Exit status is non-zero only when every post fails.

## Several posts

Give as many URLs as you like. A repeat is fetched once.

```
jasmr-dl https://japaneseasmr.com/12345/ https://japaneseasmr.com/12346/
```

`-a` reads them from a file instead, one URL per line, ignoring blank lines and
any line opening with `#`. A path of `-` reads the list from standard input.

```
jasmr-dl -a urls.txt
```

`-N` and `-j` are the run's, not each post's: five posts at `-j 32` draw on one
32 between them rather than opening 160 at the host. `-N` bounds the posts as
well as the files inside them, so `-N 1` walks through them one at a time. Every
post gets its own progress row per recording.

A post that cannot be read, or whose every download fails, is reported and the
rest carry on:

```
[done] 3 recordings saved to 2024/RJ123456
[done] 2 recordings saved to 2024/RJ123457
[done] 5 recordings from 2 posts, 1 post failed
```

## Output template

`-o` holds the path and the filename together. Its last segment always names the
file, anything before it names directories, and the path may be relative or
absolute.

```
jasmr-dl https://japaneseasmr.com/12345/ -o "{circle}/{rjcode}/{number}. {chapter}.{ext}"
jasmr-dl https://japaneseasmr.com/12345/ -o "C:/Audio/{year}/{title}/{rjcode}_{number}.{ext}"
jasmr-dl https://japaneseasmr.com/12345/ -o "<*|{number}_{chapter} [{circle}].{ext}>"
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

The default is `{rjcode}/<{rjcode}_{number}.{ext}|{number}_{chapter}.{ext}>`,
written in the same divider syntax `-o` takes: the same directory either way, a
leaf per shape.

Cover art and the gallery follow the audio: `cover.jpg` beside it, the rest
under `images/`. They come down together, on one progress line that counts both
the pictures and their bytes.

### Divider

`<A|B>` names a template for each shape a post takes: `A` per track, `B` per
chapter.

```
-o "{year}/{circle}/<{rjcode}_{number}.{ext}|{number}_{chapter}.{ext}>"
```

The divider goes anywhere in the path:

```
-o "<{year}/{title}/{rjcode}_{number}.{ext}|{date}/{rjcode}/{number}_{chapter}.{ext}>"
```

A branch of `*` keeps that side's default. Both branches cannot be `*`.

```
-o "<*|{number}_{chapter} [{circle}].{ext}>"
-o "<{year}/{title}/{rjcode}.{ext}|*>"
```

### Numbering

A template that names `{number}` places it. One that does not takes it leading
where each file is a chapter, trailing where each is a track. A post holding a
single file has nothing to count, so the counter and the separator beside it are
dropped either way.

Given `-o "{year}/{title}.{ext}"`:

| Post | Writes |
| --- | --- |
| One file | `2024/ある夏の日.mp3` |
| A file per track | `2024/ある夏の日_2.mp3` |
| A file per chapter | `2024/2_ある夏の日.mp3` |

Given `-o "{year}/{title} - {number}.{ext}"`:

| Post | Writes |
| --- | --- |
| One file | `2024/ある夏の日.mp3` |
| A file per track | `2024/ある夏の日 - 2.mp3` |

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
| `-o, --output` | `{rjcode}/<{rjcode}_{number}.{ext}\|{number}_{chapter}.{ext}>` | Template naming each file and the directories above it; `<A\|B>` uses `A` per track, `B` per chapter |
| `-P, --paths` | | Directory everything is written under |
| `-a, --batch-file` | | File listing one URL per line, or `-` for standard input |
| `-N, --concurrency` | `3` | Posts, and files within them, downloaded at once |
| `-j, --connections` | `32` | Ranged requests in flight, across every post (max 128) |
| `-R, --retries` | `4` | Retry attempts per ranged request |
| `-c, --cookies` | | Path to a `cookies.txt` export, kept for later runs |
| `--use-browser` | | Path to a browser executable that clears a Cloudflare challenge |
| `--show-browser` | `false` | Show that browser instead of running it headless |
| `-M, --no-metadata` | `false` | Do not write title, artist or album metadata |
| `-C, --no-cover` | `false` | Do not embed cover art |
| `-I, --no-images` | `false` | Do not save the rest of the post's gallery |
| `-H, --no-chapters` | `false` | Do not use the track list: no chapters, no split |
| `-S, --no-split` | `false` | Do not cut a chaptered stream into one file per chapter |
| `-v, --verbose` | `false` | Debug logging on stderr |

## Configuration

Any flag can be set outside the command line. Settings are read in this order, each one overriding the ones below it:

1. The command line
2. Environment variables
3. `jasmr-dl.conf` in the working directory
4. `config` in the per-user config directory

A config file holds one setting per line, named by its long flag without the leading dashes. Lines opening with `#` are remarks. A boolean may be written on its own. Quote a value to keep its surrounding spaces or a leading `#`.

```
# jasmr-dl.conf
concurrency = 8
connections = 64
output = {circle}/{rjcode}/{number}. {chapter}.{ext}
paths = D:\Audio
no-images
```

The environment variable for a flag is its long name uppercased, with dashes as underscores and `JASMR_DL_` in front:

```
JASMR_DL_CONCURRENCY=8
JASMR_DL_PATHS=/srv/audio
JASMR_DL_NO_IMAGES=true
```

Setting `JASMR_DL_NO_CONFIG` to any value skips both files.

## Requirements

`ffmpeg` and `ffprobe` are required for cover art, tags, and posts that serve only the site's stream. Put both on `PATH`, or beside the `jasmr-dl` binary.

Without them, use `-M -C -H` to download the audio untouched. Stream-only posts will not work.

## Cloudflare

When a challenge appears, jasmr-dl opens a browser to clear it and keeps the cookies together with the User-Agent they were earned under. Later runs reuse both. Point `--use-browser` at an executable if none is found, and pass `--show-browser` to watch it.

If that fails, export the post's cookies from a browser as `cookies.txt` and pass the path with `-c` once. The file is kept from then on.

```
jasmr-dl https://japaneseasmr.com/12345/ -c C:\path\cookies.txt
```

Cookies and the browser profile live in the per-user config directory:

| Platform | Path |
| --- | --- |
| Windows | `%AppData%\jasmr-dl` |
| macOS | `~/Library/Application Support/jasmr-dl` |
| Linux | `~/.config/jasmr-dl` |

`JASMR_DL_STATE_DIR` overrides that path. A `cookies.txt` in the working directory or beside the binary is also read.

## Embedding

`pkg/jasmrdl` is the package to import. A run takes its configuration from a `Config`, and reads and writes its Cloudflare clearance through a `Store`.

```go
sum, err := jasmrdl.Run(ctx, jasmrdl.Config{
	Targets:   targets,
	BasePath:  "/srv/audio",
	Store:     jasmrdl.FileStore{Dir: "/var/lib/jasmr-dl"},
	StoreKey:  userID + "@" + jasmrdl.Host,
	Challenge: jasmrdl.ChallengeOptions{Enabled: true, Args: jasmrdl.ContainerArgs},
})
```

`StoreKey` scopes the clearance, so each account keeps its own. `FileStore` writes one `cookies.txt` per key, `MemoryStore` holds them in the process, and any other `Store` implementation puts them elsewhere.

`Stdout`, `Stderr` and `Progress` may be nil. The returned `Summary` reports the posts, the files saved and the failures.

Chrome needs `--no-sandbox` and `--disable-dev-shm-usage` to start inside a container; `jasmrdl.ContainerArgs` holds both.

## Build

```
make build
make test
```
