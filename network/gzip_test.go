package network

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"math/rand"
	"testing"
	"time"
)

func BenchmarkGzip(b *testing.B) {
	b.Run("standard gzip", BenchmarkStandardGzip)
	b.Run("best compression gzip", BenchmarkBestCompGzip)
	b.Run("best speed gzip", BenchmarkBestSpeedGzip)
}

func BenchmarkStandardGzip(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := RandomString(1024)
		t1 := time.Now()
		cmpBytes := standardCompress([]byte(s), gzip.DefaultCompression)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
		b.ReportMetric(float64(len(s)-len(cmpBytes)), "BytesSaved")

		t1 = time.Now()
		_ = standardDecompress(cmpBytes)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "DecompressTime")
	}
}

func BenchmarkBestCompGzip(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := RandomString(1024)
		t1 := time.Now()
		cmpBytes := standardCompress([]byte(s), gzip.BestCompression)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
		b.ReportMetric(float64(len(s)-len(cmpBytes)), "BytesSaved")

		t1 = time.Now()
		_ = standardDecompress(cmpBytes)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "DecompressTime")
	}
}

func BenchmarkBestSpeedGzip(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := RandomString(1024)
		t1 := time.Now()
		cmpBytes := standardCompress([]byte(s), gzip.BestSpeed)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "CompressTime")
		b.ReportMetric(float64(len(s)-len(cmpBytes)), "BytesSaved")

		t1 = time.Now()
		_ = standardDecompress(cmpBytes)
		b.ReportMetric(float64(time.Since(t1).Nanoseconds()), "DecompressTime")
	}
}

func standardCompress(msg []byte, level int) []byte {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		fmt.Printf("Error when creating gzip writer: %s\n", err)
		return nil
	}

	_, err = writer.Write(msg)
	if err != nil {
		fmt.Printf("Error when writing bytes to gzip: %s\n", err)
		return nil
	}

	err = writer.Flush()
	if err != nil {
		fmt.Printf("Error when flushing bytes to gzip: %s\n", err)
		return nil
	}

	err = writer.Close()
	if err != nil {
		fmt.Printf("Error when flushing bytes to gzip: %s\n", err)
		return nil
	}
	return buf.Bytes()
}

func standardDecompress(msg []byte) []byte {
	bReader := bytes.NewReader(msg)
	reader, err := gzip.NewReader(bReader)
	if err != nil {
		fmt.Printf("Error when creating gzip reader: %s\n", err)
		return nil
	}

	b, err := ioutil.ReadAll(reader)
	if err != nil {
		fmt.Printf("Error when reading gzipped data: %s\n", err)
		return nil
	}

	err = reader.Close()
	if err != nil {
		fmt.Printf("Error when closing gzip reader: %s\n", err)
		return nil
	}
	return b
}

func RandomString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 !%$*#@|/.,<>?[]{}-=_+()&^")

	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}

func TestStandardCompressDecompress(t *testing.T) {
	s := RandomString(1024)
	sBytes := []byte(s)
	cmpBytes := standardCompress(sBytes, gzip.DefaultCompression)
	fmt.Printf("len, compLen %d, %d\n", len(sBytes), len(cmpBytes))
	dcmpBytes := standardDecompress(cmpBytes)
	dcmpString := string(dcmpBytes)
	if dcmpString != s {
		t.Fatalf("decompressed string not equal to original string\ndecomp string: \t `%s`\noriginal string: \t `%s`\n", dcmpString, s)
	}
}
