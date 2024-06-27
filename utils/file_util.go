package utils

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"github.com/banbox/banexg/errs"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func CopyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err = EnsureDir(dst, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(src, entry.Name())
		destPath := filepath.Join(dst, entry.Name())

		fileInfo, err := os.Lstat(sourcePath)
		if err != nil {
			return err
		}

		switch fileInfo.Mode() & os.ModeType {
		case os.ModeDir:
			if err = EnsureDir(destPath, 0755); err != nil {
				return err
			}
			if err = CopyDir(sourcePath, destPath); err != nil {
				return err
			}
		case os.ModeSymlink:
			if err = CopySymLink(sourcePath, destPath); err != nil {
				return err
			}
		default:
			if err = Copy(sourcePath, destPath); err != nil {
				return err
			}
		}

		fInfo, err := entry.Info()
		if err != nil {
			return err
		}

		isSymlink := fInfo.Mode()&os.ModeSymlink != 0
		if !isSymlink {
			if err = os.Chmod(destPath, fInfo.Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}

func Copy(srcFile, dstFile string) error {
	out, err := os.Create(dstFile)
	if err != nil {
		return err
	}

	defer out.Close()

	in, err := os.Open(srcFile)
	if err != nil {
		return err
	}

	defer in.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return nil
}

func Exists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

func EnsureDir(dir string, perm os.FileMode) error {
	if Exists(dir) {
		return nil
	}

	if err := os.MkdirAll(dir, perm); err != nil {
		return fmt.Errorf("failed to create directory: '%s', error: '%s'", dir, err.Error())
	}

	return nil
}

func CopySymLink(source, dest string) error {
	link, err := os.Readlink(source)
	if err != nil {
		return err
	}
	return os.Symlink(link, dest)
}

func WriteCsvFile(path string, rows [][]string, compress bool) *errs.Error {
	var fileWriter io.Writer
	var err_ error
	if compress {
		zipFile, err_ := os.Create(strings.Replace(path, ".csv", ".zip", 1))
		if err_ != nil {
			return errs.New(errs.CodeIOWriteFail, err_)
		}
		zipWriter := zip.NewWriter(zipFile)
		defer zipWriter.Close()
		header := &zip.FileHeader{
			Name:     filepath.Base(path),
			Method:   zip.Deflate,
			Modified: time.Now(),
		}
		fileWriter, err_ = zipWriter.CreateHeader(header)
		if err_ != nil {
			return errs.New(errs.CodeIOWriteFail, err_)
		}
	} else {
		file, err_ := os.Create(path)
		if err_ != nil {
			return errs.New(errs.CodeIOWriteFail, err_)
		}
		defer file.Close()
		fileWriter = file
	}
	writer := csv.NewWriter(fileWriter)
	defer writer.Flush()
	err_ = writer.WriteAll(rows)
	if err_ != nil {
		return errs.New(errs.CodeIOWriteFail, err_)
	}
	return nil
}
