package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	getopt "github.com/pborman/getopt/v2"
	"github.com/rs/zerolog"
)

var (
	reName = regexp.MustCompile(`^[A-Za-z_][0-9A-Za-z]+(?:[_.-][0-9A-Za-z]+)*$`)
	reID   = regexp.MustCompile(`^(?:0|[1-9][0-9]*)$`)
)

var (
	Logger zerolog.Logger

	flagLogJSON bool
	flagSrcFile string
	flagDstFile string
	flagColumn  int
	flagNumeric bool
)

func init() {
	getopt.SetParameters("")
	getopt.FlagLong(&flagLogJSON, "log-json", 'J', "log JSON to stderr").SetFlag()
	getopt.FlagLong(&flagSrcFile, "source-file", 's', "passwd-like file to read from").Mandatory()
	getopt.FlagLong(&flagDstFile, "dest-file", 'd', "libnss-cache index file to create").Mandatory()
	getopt.FlagLong(&flagColumn, "column", 'c', "column in --source-file to index").Mandatory()
	getopt.FlagLong(&flagNumeric, "numeric", 'n', "set if the specified column is numeric").SetFlag()
}

func main() {
	getopt.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.DurationFieldUnit = time.Second
	zerolog.DurationFieldInteger = false
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	var out io.Writer = zerolog.ConsoleWriter{Out: os.Stderr}
	if flagLogJSON {
		out = os.Stderr
	}
	Logger = zerolog.
		New(out).
		Level(zerolog.InfoLevel)

	if flagColumn < 1 || flagColumn > 9 {
		Logger.Fatal().
			Int("column", flagColumn).
			Msg("columns are numbered starting from 1 to a maximum of 9")
	}

	Logger.Info().
		Str("source-file", flagSrcFile).
		Msg("opening source text database file")
	src, err := os.OpenFile(flagSrcFile, os.O_RDONLY, 0)
	if err != nil {
		Logger.Fatal().
			Str("source-file", flagSrcFile).
			Err(err).
			Msg("failed to open file")
	}

	defer src.Close()

	fi, err := src.Stat()
	if err != nil {
		Logger.Fatal().
			Str("source-file", flagSrcFile).
			Err(err).
			Msg("failed to stat source file")
	}

	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		Logger.Fatal().
			Str("source-file", flagSrcFile).
			Msgf("source file's io/fs.FileInfo object was backed by %T, not *syscall.Stat_t", fi.Sys())
	}

	rawBytes, err := ioutil.ReadAll(src)
	if err != nil {
		Logger.Fatal().
			Str("source-file", flagSrcFile).
			Err(err).
			Msg("failed to read from source file")
	}

	keys := make([]string, 0, 1024)
	index := make(map[string]string, 1024)
	longestEntryLength := 2

	var fileOffset int
	var lineNumber int
	var eof bool
	for !eof {
		lineOffset := fileOffset
		for {
			// Stop if the last line of the file does NOT end in LF
			if fileOffset >= len(rawBytes) {
				eof = true
				break
			}

			// Detect LF
			ch := rawBytes[fileOffset]
			fileOffset++
			if ch == '\n' {
				break
			}
		}

		lineNumber++
		line := string(rawBytes[lineOffset:fileOffset])
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		columns := strings.Split(line, ":")
		if flagColumn > len(columns) {
			Logger.Fatal().
				Str("source-file", flagSrcFile).
				Int("line-offset", lineOffset).
				Int("line-number", lineNumber).
				Int("column", flagColumn).
				Int("min", 0).
				Int("max", len(columns)).
				Msg("--column flag exceeds the number of available columns in the --source-file")
		}

		key := columns[flagColumn-1]
		if flagNumeric {
			if !reID.MatchString(key) {
				Logger.Fatal().
					Str("source-file", flagSrcFile).
					Int("line-offset", lineOffset).
					Int("line-number", lineNumber).
					Int("column", flagColumn).
					Str("text", key).
					Msg("invalid numeric identifier")
			}
		} else {
			if !reName.MatchString(key) {
				Logger.Fatal().
					Str("source-file", flagSrcFile).
					Int("line-offset", lineOffset).
					Int("line-number", lineNumber).
					Int("column", flagColumn).
					Str("text", key).
					Msg("invalid user or group name")
			}
		}

		lineOffsetString := strconv.FormatInt(int64(lineOffset), 10)
		keys = append(keys, key)
		index[key] = lineOffsetString
		entryLength := 2 + len(key) + len(lineOffsetString)
		if longestEntryLength < entryLength {
			longestEntryLength = entryLength
		}
	}

	sort.Strings(keys)
	Logger.Info().Msgf("found %d rows in --source-file", len(keys))

	tempFile := flagDstFile + "~"

	Logger.Info().
		Str("dest-file", flagDstFile).
		Str("temp-file", tempFile).
		Msg("opening destination binary index file")
	dst, err := os.OpenFile(tempFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		Logger.Fatal().
			Str("temp-file", tempFile).
			Err(err).
			Msg("failed to open file")
	}

	needCleanup := true
	defer func() {
		if needCleanup {
			_ = dst.Close()
			_ = os.Remove(tempFile)
		}
	}()

	var buf bytes.Buffer
	buf.Grow(longestEntryLength + 1)
	for _, key := range keys {
		offset := index[key]

		buf.WriteString(key)
		buf.WriteByte(0)
		buf.WriteString(offset)
		buf.WriteByte(0)

		entryLength := 2 + len(key) + len(offset)
		pad := longestEntryLength - entryLength
		for pad > 0 {
			buf.WriteByte(0)
			pad--
		}

		buf.WriteByte('\n')

		_, err = dst.Write(buf.Bytes())
		if err != nil {
			Logger.Fatal().
				Str("temp-file", tempFile).
				Err(err).
				Msg("failed to write to file")
		}

		buf.Reset()
	}

	err = dst.Chown(int(st.Uid), int(st.Gid))
	if err != nil {
		Logger.Fatal().
			Str("temp-file", tempFile).
			Uint32("uid", st.Uid).
			Uint32("gid", st.Gid).
			Err(err).
			Msg("failed to chown file")
	}

	err = dst.Chmod(os.FileMode(st.Mode & 07777))
	if err != nil {
		Logger.Fatal().
			Str("temp-file", tempFile).
			Str("mode", fmt.Sprintf("0o%04o", st.Mode&07777)).
			Err(err).
			Msg("failed to chmod file")
	}

	err = dst.Close()
	if err != nil {
		Logger.Fatal().
			Str("temp-file", tempFile).
			Err(err).
			Msg("failed to close file")
	}

	needCleanup = false

	err = os.Rename(tempFile, flagDstFile)
	if err != nil {
		Logger.Fatal().
			Str("dest-file", flagDstFile).
			Str("temp-file", tempFile).
			Err(err).
			Msg("failed to replace destination file with temporary file")
	}
}
