# jasmr-dl

Download audio from [japaneseasmr.com](https://japaneseasmr.com) posts, with metadata, cover art and chapters on request.

## Features

- Downloads several posts, and every part of each one, in parallel
- Reads a list of URLs from a file
- Embeds cover art and writes title, artist, circle, RJ code, date and track numbers
- Records the post URL and its tags in the comment field
- Cuts a chaptered stream into one file per chapter, or embeds the track list as chapter markers
- Saves the cover art and the post's image gallery alongside the audio
- Writes an `album.nfo` for Kodi, Jellyfin and Emby
- Writes the whole post to a JSON file, and applies that file to the recordings later
- Handles posts that serve only the site's stream
- Names files and directories from a template
- Rewrites what a post says about itself before any of it is written
- Clears Cloudflare challenges in a browser and reuses the clearance

## Requirements

`ffmpeg` and `ffprobe` are required by every `--embed-` flag, by `--split-chapters`, by `--load-info-json`, and by posts that serve only the site's stream. Put both on `PATH`, or beside the `jasmr-dl` binary.

Without them a run downloads the audio untouched, and `--write-cover`, `--write-images`, `--write-nfo` and `--write-info-json` still work. Stream-only posts will not.

## Installation

Download a binary for your platform from [Releases](https://github.com/EagleStelle/jasmr-dl/releases).

Or with Go:

```
go install github.com/EagleStelle/jasmr-dl@latest
```

## Usage

```
jasmr-dl https://japaneseasmr.com/12345/
```

The audio lands under the post's RJ code, untouched:

```
RJ123456/RJ123456_1.mp3
RJ123456/RJ123456_2.mp3
```

Everything beyond the audio is asked for. Tags and art:

```
jasmr-dl https://japaneseasmr.com/12345/ --embed-metadata --embed-cover --write-cover
```

```
RJ123456/RJ123456_1.mp3
RJ123456/RJ123456_2.mp3
RJ123456/cover.jpg
```

A post that serves a chaptered stream can be cut into its chapters, and the
gallery saved beside them:

```
jasmr-dl https://japaneseasmr.com/12345/ --split-chapters --write-images
```

```
RJ123456/01_はじめに.m4a
RJ123456/02_耳かき.m4a
RJ123456/images/01.jpg
```

Go faster with more ranged requests in flight, and more files at once:

```
jasmr-dl https://japaneseasmr.com/12345/ -j 64 -N 8
```

`Ctrl+C` cancels. Exit status is non-zero only when every post fails.

### Multiple posts

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

## Options

| Flag | Default | Description |
| --- | --- | --- |
| `-o, --output` | `{rjcode}/<{rjcode}_{number}.{ext}\|{number}_{chapter}.{ext}>` | Template naming each file and the directories above it; `<A\|B>` uses `A` per track, `B` per chapter |
| `--parse-metadata` | | Rewrite the post's metadata, as `FROM:TO`; repeatable |
| `-P, --paths` | | Directory everything is written under |
| `-a, --batch-file` | | File listing one URL per line, or `-` for standard input |
| `-N, --concurrency` | `3` | Posts, and files within them, downloaded at once |
| `-j, --connections` | `32` | Ranged requests in flight, across every post (max 128) |
| `-R, --retries` | `4` | Retry attempts per ranged request |
| `-c, --cookies` | | Path to a `cookies.txt` export, kept for later runs |
| `--use-browser` | | Path to a browser executable that clears a Cloudflare challenge |
| `--show-browser` | `false` | Show that browser instead of running it headless |
| `--write-info-json` | `false` | Write the post's metadata and chapters to a JSON file |
| `--write-cover` | `false` | Save the post's cover art |
| `--write-images` | `false` | Save the rest of the post's gallery |
| `--write-nfo` | `false` | Write an `album.nfo` for a media server to read |
| `--embed-metadata` | `false` | Write title, artist and album tags into each file |
| `--embed-cover` | `false` | Embed the cover art in each file |
| `--embed-chapters` | `false` | Write the track list into the file as chapter markers |
| `--split-chapters` | `false` | Cut a chaptered stream into one file per chapter |
| `--load-info-json` | | Path to a written JSON file, applied to the recordings beside it |
| `-v, --verbose` | `false` | Debug logging on stderr |

A boolean flag is turned off again with `=false`, which is how a run overrides
what a config file or an environment variable set:

```
jasmr-dl https://japaneseasmr.com/12345/ --write-images=false
```

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
embed-metadata
embed-cover
split-chapters
```

The environment variable for a flag is its long name uppercased, with dashes as underscores and `JASMR_DL_` in front:

```
JASMR_DL_CONCURRENCY=8
JASMR_DL_PATHS=/srv/audio
JASMR_DL_EMBED_METADATA=true
```

Setting `JASMR_DL_NO_CONFIG` to any value skips both files.

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

<a id="fields"></a>

| Field | Names | |
| --- | --- | --- |
| `{title}` | Post title | post |
| `{rjcode}` | DLsite code | post |
| `{circle}` | Circle | post |
| `{artist}` | Voice actor | post |
| `{album}` | Album name, `Title [RJ123456]` | post |
| `{genre}` | Genre | post |
| `{date}` `{year}` `{month}` `{day}` | Post date | post |
| `{number}` | File number | file |
| `{chapter}` | Chapter title | file |
| `{track}` `{tracktotal}` | Track number | file |
| `{ext}` | File extension | file |

A field the post does not carry writes `Unknown`. The file fields belong to the
filename, not a directory. The post fields are the same ones
[`--parse-metadata`](#rewriting-metadata) is written in.

The default is `{rjcode}/<{rjcode}_{number}.{ext}|{number}_{chapter}.{ext}>`,
written in the same divider syntax `-o` takes: the same directory either way, a
leaf per shape.

Cover art and the gallery follow the audio: `cover.jpg` beside it, the rest
under `images/`. They come down together, on one progress line that counts both
the pictures and their bytes. `album.nfo` and the `.info.json` sit in the same
directory as the audio.

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

A post serves one of two shapes: separate files, or one stream with a track list
beside it. Only the second has chapters, and only where the whole work is that
one stream.

| | |
| --- | --- |
| neither flag | One file, no track list written anywhere |
| `--embed-chapters` | One file, the track list embedded as chapter markers |
| `--split-chapters` | One file per chapter, each titled for it |

`--split-chapters` wins where both are named: every piece is one chapter
already, so there is nothing left to mark inside it. A post that already serves
separate files is left alone by both.

A chapter the stream ends before is reported and skipped, and the last chapter
runs to the file's real length rather than what the playlist claimed.

## Metadata

Nothing is written into a file, or beside it, unless the run asks for it.

| Flag | Writes |
| --- | --- |
| `--embed-metadata` | Title, artist, circle, album, date, genre and track numbers into each file, with the post URL and its tags in the comment |
| `--embed-cover` | The cover art into each file, padded square, as JPEG |
| `--embed-chapters` | The track list into the file as chapter markers |
| `--write-cover` | `cover.jpg` beside the audio |
| `--write-images` | The rest of the post's gallery under `images/` |
| `--write-nfo` | `album.nfo`, read by Kodi, Jellyfin, and Emby |
| `--write-info-json` | `RJ123456.info.json`, the whole post as JSON |

`--embed-cover` fetches the art whether or not `--write-cover` keeps it, and
removes what it fetched once the post is done.

Embedding anything needs `ffmpeg`; the four `--write-` flags do not.

The album is named `Title [RJ123456]` wherever it is written — embedded tags,
`album.nfo` and the `.info.json` alike. A post carrying only the one half is
named for that half alone.

### Rewriting metadata

`--parse-metadata FROM:TO` changes what a post says about itself before any of
it is written. `FROM` names the fields to read, `TO` is the pattern that text
has to match, and each `{field}` in `TO` captures what that field becomes. It is
yt-dlp's flag of the same name, written in the [fields](#fields) `-o` takes
rather than yt-dlp's `%(field)s`.

```
jasmr-dl https://japaneseasmr.com/12345/ --embed-metadata --parse-metadata "{title}:{artist} - {title}"
```

A post titled `CV.鈴木 - ある夏の日` is read as `{artist}` = `CV.鈴木` and
`{title}` = `ある夏の日`, and both are written that way. The flag is repeatable,
the rules running in the order they are given.

Every post field can be read; every one but `{year}`, `{month}` and `{day}` can
be written, those three following `{date}` rather than leading it. The file
fields name no post, so a rule cannot use them.

`TO` reads one of two ways, and they cannot be mixed. A `TO` naming fields is a
template, the text around each `{field}` standing for itself. A `TO` naming none
is a regular expression, its `(?P<field>…)` groups saying what to keep.

```
--parse-metadata "{title}:【{circle}】{title}"
--parse-metadata "{title}:^(?P<rjcode>RJ\d+) (?P<title>.+)$"
```

A bare field name is that field on the left, and keeps the whole of what was
matched on the right, as in `title:album`. A rule is cut at its first colon; one
belonging to either half is written `\:`. A group that takes no part in the
match leaves its field alone, and one matching an empty string empties it.

`{album}` follows `{title}` and `{rjcode}` unless a rule names it. A rule whose
pattern matches nothing leaves the post as it stands, which is what lets one
rule serve a batch of posts it only sometimes fits; `-v` reports which matched.

The rules apply to `--load-info-json` too, over each file the record names. They
never move a file: `-o` reads the page rather than the rewritten metadata.

### album.nfo

`--write-nfo` writes one `album.nfo` per post directory. Several values go out
twice, under the name each server knows: `<plot>` for Jellyfin and Emby beside
`<review>` for Kodi, `<premiered>` beside `<releasedate>`, and each of the post's
tags as both `<tag>` and the `<style>` Kodi's music scraper reads. A server
ignores what it does not know, so every reader gets the value.

The post URL and its tag list have no album element of their own, so they go in
the plot, where every server will at least display them. The RJ code goes out as
a `<uniqueid type="rjcode">`.

### info.json

`--write-info-json` records the whole post: title, circle, artist, date, tags,
the URL, the chapter list, and the metadata for every file it wrote. It records
all of that whatever the run embedded, so a download that wrote no tags still
leaves everything needed to write them later.

```
jasmr-dl https://japaneseasmr.com/12345/ --embed-metadata --write-info-json --write-nfo
```

`--load-info-json` writes a record back into the recordings beside it, fetching
nothing:

```
jasmr-dl --load-info-json RJ123456/RJ123456.info.json
```

On its own that writes everything the record holds. Naming an embed flag narrows
it to what was named:

```
jasmr-dl --load-info-json RJ123456/RJ123456.info.json --embed-chapters
```

Files are read relative to the record, so a directory that was moved as a whole
still applies. A file the record names but the disk does not carry is reported
and the rest carry on. `--load-info-json` takes no URL.

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

## Library usage

`pkg/jasmrdl` is the package to import. A run takes its configuration from a `Config`, and reads and writes its Cloudflare clearance through a `Store`.

```go
sum, err := jasmrdl.Run(ctx, jasmrdl.Config{
	Targets:       targets,
	BasePath:      "/srv/audio",
	EmbedMetadata: true,
	EmbedCover:    true,
	SplitChapters: true,
	WriteInfoJSON: true,
	Store:         jasmrdl.FileStore{Dir: "/var/lib/jasmr-dl"},
	StoreKey:      userID + "@" + jasmrdl.Host,
	Challenge:     jasmrdl.ChallengeOptions{Enabled: true, Args: jasmrdl.ContainerArgs},
})
```

The `Write` and `Embed` fields are the flags of the same name, and default to
off exactly as they do. `LoadInfoJSON` names a record to write back instead of
downloading, in which case `Targets` must be empty.

`StoreKey` scopes the clearance, so each account keeps its own. `FileStore` writes one `cookies.txt` per key, `MemoryStore` holds them in the process, and any other `Store` implementation puts them elsewhere.

`Stdout`, `Stderr` and `Progress` may be nil. The returned `Summary` reports the posts, the files saved and the failures.

Chrome needs `--no-sandbox` and `--disable-dev-shm-usage` to start inside a container; `jasmrdl.ContainerArgs` holds both.

## Development

```
make build
make test
```
