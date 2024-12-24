package utils

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/flopp/go-findfont"
	"github.com/xuri/excelize/v2"
	"golang.org/x/image/font/opentype"
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

// FindSubPath searches for the first occurrence of a directory named tgtName
// within the specified workDir and its subdirectories up to two levels deep.
func FindSubPath(parDir, tgtName string, maxDepth int) (string, error) {
	if maxDepth < 1 {
		return "", fmt.Errorf("maxDepth must >= 1")
	}
	if _, err := os.Stat(parDir); os.IsNotExist(err) {
		return "", fmt.Errorf("directory %s does not exist", parDir)
	}
	var tgtPath string
	err := filepath.Walk(parDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(parDir, path)
		if err != nil {
			return err
		}
		// calculate depth of current path
		depth := len(strings.Split(filepath.ToSlash(relativePath), "/"))
		if depth > maxDepth {
			return filepath.SkipDir
		}

		if info.Name() == tgtName {
			tgtPath = path
			return filepath.SkipDir
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	if tgtPath == "" {
		return "", fmt.Errorf("target directory %s not found within two levels of %s", tgtName, parDir)
	}

	return tgtPath, nil
}

func CopySymLink(source, dest string) error {
	link, err := os.Readlink(source)
	if err != nil {
		return err
	}
	return os.Symlink(link, dest)
}

func MovePath(src, tgt string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	tgtAbs, err := filepath.Abs(tgt)
	if err != nil {
		return err
	}

	_, err = os.Stat(srcAbs)
	if os.IsNotExist(err) {
		return fmt.Errorf("Source Not Exist: %v" + src)
	}

	tgtDir := filepath.Dir(tgtAbs)
	if err = os.MkdirAll(tgtDir, 0755); err != nil {
		return err
	}

	// try rename/move directly
	err = os.Rename(srcAbs, tgtAbs)

	// copy and delete if move fail
	if err != nil {
		if err = copyPath(srcAbs, tgtAbs); err != nil {
			return err
		}

		if err = os.RemoveAll(srcAbs); err != nil {
			return err
		}
	}
	return nil
}

// copyPath 递归复制文件或文件夹
func copyPath(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// 如果是文件夹，递归复制
	if srcInfo.IsDir() {
		if err = os.MkdirAll(dst, srcInfo.Mode()); err != nil {
			return err
		}

		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			srcPath := filepath.Join(src, entry.Name())
			dstPath := filepath.Join(dst, entry.Name())

			if entry.IsDir() {
				if err := copyPath(srcPath, dstPath); err != nil {
					return err
				}
			} else {
				if err := copyFile(srcPath, dstPath); err != nil {
					return err
				}
			}
		}
	} else {
		// 如果是文件，直接复制
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	srcContent, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, srcContent, 0644)
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

func WriteFile(path string, data []byte) *errs.Error {
	file, err_ := os.Create(path)
	if err_ != nil {
		return errs.New(errs.CodeIOWriteFail, err_)
	}
	defer file.Close()
	_, err_ = file.Write(data)
	if err_ != nil {
		return errs.New(errs.CodeIOWriteFail, err_)
	}
	return nil
}

func KlineToStr(klines []*banexg.Kline, loc *time.Location) [][]string {
	rows := make([][]string, 0, len(klines))
	for _, k := range klines {
		var dateStr string
		if loc != nil {
			dateStr = btime.ToTime(k.Time).In(loc).Format(core.DefaultDateFmt)
		} else {
			dateStr = strconv.FormatInt(k.Time/1000, 10)
		}
		row := []string{
			dateStr,
			strconv.FormatFloat(k.Open, 'f', -1, 64),
			strconv.FormatFloat(k.High, 'f', -1, 64),
			strconv.FormatFloat(k.Low, 'f', -1, 64),
			strconv.FormatFloat(k.Close, 'f', -1, 64),
			strconv.FormatFloat(k.Volume, 'f', -1, 64),
			strconv.FormatFloat(k.Info, 'f', -1, 64),
		}
		rows = append(rows, row)
	}
	return rows
}

func ReadLastNLines(filePath string, lineCount int) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var result []string

	// 获取文件大小
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := fileInfo.Size()

	// 设定缓冲区大小和读取偏移量
	bufferSize := 4096
	var offset = fileSize
	var buffer []byte

	var tmp string
	for offset > 0 && len(result) < lineCount {
		if int64(bufferSize) > offset {
			bufferSize = int(offset)
			offset = 0
		} else {
			offset -= int64(bufferSize)
		}

		buffer = make([]byte, bufferSize)
		_, err = file.ReadAt(buffer, offset)
		if err != nil {
			return nil, err
		}

		lines := strings.Split(string(buffer), "\n")
		if len(lines) > 0 {
			lines[len(lines)-1] += tmp
			tmp = lines[0]
			lines = lines[1:]
		} else {
			tmp = ""
		}
		// 倒序读取行
		for i := len(lines) - 1; i >= 0; i-- {
			if len(result) < lineCount {
				if lines[i] != "" {
					result = append(result, lines[i])
				}
			} else {
				break
			}
		}
	}

	// 倒序返回结果
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result, nil
}

func ReadCSV(path string) ([][]string, *errs.Error) {
	file, err_ := os.Open(path)
	if err_ != nil {
		return nil, errs.New(errs.CodeIOReadFail, err_)
	}
	defer file.Close()
	rows, err_ := csv.NewReader(file).ReadAll()
	if err_ != nil {
		return nil, errs.New(errs.CodeIOReadFail, err_)
	}
	return rows, nil
}

func ReadTextFile(path string) (string, *errs.Error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", errs.New(errs.CodeIOReadFail, err)
	}

	if info.IsDir() {
		return "", errs.NewMsg(errs.CodeIOReadFail, "File is a directory")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", errs.New(errs.CodeIOReadFail, err)
	}

	if !IsTextContent(content) {
		return "", errs.NewMsg(errs.CodeIOReadFail, "File is not a text file")
	}

	return string(content), nil
}

/*
ReadXlsx use first if `sheet` is empty
*/
func ReadXlsx(path, sheet string) ([][]string, *errs.Error) {
	f, err_ := excelize.OpenFile(path)
	if err_ != nil {
		return nil, errs.New(errs.CodeIOReadFail, err_)
	}
	defer f.Close()
	if sheet == "" {
		sheet = f.GetSheetName(0)
	}
	rows, err_ := f.Rows(sheet)
	if err_ != nil {
		return nil, errs.New(errs.CodeIOReadFail, err_)
	}
	var res [][]string
	for rows.Next() {
		opts := rows.GetRowOpts()
		if opts.Hidden {
			continue
		}
		cells, err_ := rows.Columns()
		if err_ != nil {
			return nil, errs.New(errs.CodeIOReadFail, err_)
		}
		if len(cells) > 1 {
			// skip row index
			res = append(res, cells[1:])
		}
	}
	return res, nil
}

func GetFontData(name string) ([]byte, error) {
	if name == "" {
		name = "arial.ttf"
	}
	path, err := findfont.Find(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetOpenFont(name string) (*opentype.Font, error) {
	fontData, err := GetFontData(name)
	if err != nil {
		return nil, err
	}
	fontFace, err := opentype.Parse(fontData)
	return fontFace, err
}

func GetFilesWithPrefix(filePath string) ([]string, error) {
	dir := filepath.Dir(filePath)
	fileName := filepath.Base(filePath)
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// 判断文件路径是否以指定的前缀开头
		if strings.HasPrefix(filepath.Base(path), fileName) {
			files = append(files, path)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}
