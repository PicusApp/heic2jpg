package main

import (
	"image/jpeg"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/fanyang89/zerologging/v1"
	"github.com/jdeng/goheif"
	"github.com/jdeng/goheif/heif"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

func newWriterExif(w io.Writer, exif []byte) (io.Writer, error) {
	writer := &writerSkipper{w, 2}
	soi := []byte{0xff, 0xd8}
	if _, err := w.Write(soi); err != nil {
		return nil, err
	}

	if exif != nil {
		app1Marker := 0xe1
		markerLen := 2 + len(exif)
		marker := []byte{0xff, uint8(app1Marker), uint8(markerLen >> 8), uint8(markerLen & 0xff)}
		if _, err := w.Write(marker); err != nil {
			return nil, err
		}

		if _, err := w.Write(exif); err != nil {
			return nil, err
		}
	}

	return writer, nil
}

func convertHeif(r *os.File, writer io.Writer) error {
	exif, err := goheif.ExtractExif(r)
	if err != nil && !errors.Is(err, heif.ErrNoEXIF) {
		return errors.Wrap(err, "extract exif failed")
	}

	img, err := goheif.Decode(r)
	if err != nil {
		return errors.Wrap(err, "decode failed")
	}

	w, _ := newWriterExif(writer, exif)
	err = jpeg.Encode(w, img, nil)
	if err != nil {
		return errors.Wrap(err, "encode failed")
	}

	return nil
}

func convertFile(p string) error {
	in, err := os.Open(p)
	if err != nil {
		return errors.Wrapf(err, "open failed, path: %s", p)
	}

	ext := filepath.Ext(p)
	outPath := strings.TrimSuffix(p, ext) + ".jpg"
	out, err := os.OpenFile(outPath, os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		_ = in.Close()
		return errors.Wrapf(err, "open failed, path: %s")
	}

	err = convertHeif(in, out)
	_ = in.Close()
	_ = out.Close()
	if err != nil {
		return err
	}

	log.Info().Str("source", p).Msg("Converted")
	return nil
}

func run(c *cli.Context) error {
	args := c.Args()
	for i := 0; i < args.Len(); i++ {
		p := args.Get(i)

		s, err := os.Stat(p)
		if err != nil {
			return errors.Wrapf(err, "stat failed, path: %s", p)
		}

		if s.IsDir() {
			err = filepath.Walk(p, func(path string, info fs.FileInfo, err error) error {
				if err != nil {
					log.Warn().Err(err).Msg("walk failed")
					return filepath.SkipDir
				}
				if !info.IsDir() && strings.HasSuffix(info.Name(), ".heic") {
					return convertFile(path)
				}
				return nil
			})
		} else {
			err = convertFile(p)
		}

		if err != nil {
			return err
		}
	}
	return nil
}

// Skip Writer for exif writing
type writerSkipper struct {
	w           io.Writer
	bytesToSkip int
}

func (w *writerSkipper) Write(data []byte) (int, error) {
	if w.bytesToSkip <= 0 {
		return w.w.Write(data)
	}

	if dataLen := len(data); dataLen < w.bytesToSkip {
		w.bytesToSkip -= dataLen
		return dataLen, nil
	}

	if n, err := w.w.Write(data[w.bytesToSkip:]); err == nil {
		n += w.bytesToSkip
		w.bytesToSkip = 0
		return n, nil
	} else {
		return n, err
	}
}

func main() {
	zerologging.WithConsoleLog(zerolog.InfoLevel)

	app := &cli.App{
		Name:   "heic2jpg",
		Action: run,
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal().Err(err).Msg("")
	}
}
