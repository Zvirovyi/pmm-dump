package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"io"
	"os"
	"pmm-dump/pkg/dump"
	"pmm-dump/pkg/grafana"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/valyala/fasthttp"
)

const minPMMServerVersion = "2.12.0"

func newClientHTTP(insecureSkipVerify bool) *fasthttp.Client {
	return &fasthttp.Client{
		MaxConnsPerHost:           2,
		MaxIdleConnDuration:       time.Minute,
		MaxIdemponentCallAttempts: 5,
		ReadTimeout:               time.Minute,
		WriteTimeout:              time.Minute,
		MaxConnWaitTimeout:        time.Second * 30,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: insecureSkipVerify,
		},
	}
}

type goroutineLoggingHook struct{}

func (h goroutineLoggingHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	e.Int("goroutine_id", getGoroutineID())
}

func getGoroutineID() int {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	id, err := strconv.Atoi(idField)
	if err != nil {
		panic(fmt.Sprintf("cannot get goroutine id: %v", err))
	}
	return id
}

// getPMMVersion returns version, full-version and error
func getPMMVersion(pmmURL string, c grafana.Client) (string, string, error) {
	type versionResp struct {
		Version string `json:"version"`
		Server  struct {
			Version     string    `json:"version"`
			FullVersion string    `json:"full_version"`
			Timestamp   time.Time `json:"timestamp"`
		} `json:"server"`
		Managed struct {
			Version     string    `json:"version"`
			FullVersion string    `json:"full_version"`
			Timestamp   time.Time `json:"timestamp"`
		} `json:"managed"`
		DistributionMethod string `json:"distribution_method"`
	}

	statusCode, body, err := c.Get(fmt.Sprintf("%s/v1/version", pmmURL))

	if err != nil {
		return "", "", err
	}
	if statusCode != fasthttp.StatusOK {
		return "", "", fmt.Errorf("non-ok status: %d", statusCode)
	}
	resp := new(versionResp)
	if err = json.Unmarshal(body, resp); err != nil {
		return "", "", fmt.Errorf("failed to unmarshal response: %s", err)
	}
	return resp.Server.Version, resp.Server.FullVersion, nil
}

// getTimeZone returns empty string result if there is no preferred timezone in pmm-server graphana settings
func getPMMTimezone(pmmURL string, c grafana.Client) (string, error) {
	type tzResp struct {
		Timezone string `json:"timezone"`
	}

	statusCode, body, err := c.Get(fmt.Sprintf("%s/graph/api/org/preferences", pmmURL))
	if err != nil {
		return "", err
	}
	if statusCode != fasthttp.StatusOK {
		return "", fmt.Errorf("non-ok status: %d", statusCode)
	}

	resp := new(tzResp)
	if err = json.Unmarshal(body, resp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %s", err)
	}
	return resp.Timezone, nil
}

func composeMeta(pmmURL string, c grafana.Client) (*dump.Meta, error) {
	_, pmmVer, err := getPMMVersion(pmmURL, c)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get PMM version")
	}

	pmmTzRaw, err := getPMMTimezone(pmmURL, c)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get PMM timezone")
	}
	var pmmTz *string
	if len(pmmTzRaw) == 0 || pmmTzRaw == "browser" {
		pmmTz = nil
	} else {
		pmmTz = &pmmTzRaw
	}

	var args string
	for i, v := range os.Args[1:] {
		if i != 0 {
			args += " "
		}
		// Only i and not i-1 because we are going by [1:] slice
		switch os.Args[i] {
		case "--pmm-url":
			args += pmmURL
		case "--pmm-user":
			args += "***"
		case "--pmm-pass":
			args += "***"
		default:
			args += v
		}
	}

	meta := &dump.Meta{
		Version: dump.PMMDumpVersion{
			GitBranch: GitBranch,
			GitCommit: GitCommit,
		},
		PMMServerVersion: pmmVer,
		PMMTimezone:      pmmTz,
		Arguments:        args,
	}

	return meta, nil
}

func ByteCountDecimal(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

func ByteCountBinary(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func checkPiped() (bool, error) {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false, err
	}
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		return true, nil
	}
	return false, nil
}

type LevelWriter struct {
	Writer io.Writer
	Level  zerolog.Level
}

func (lw LevelWriter) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	if level >= lw.Level {
		return lw.Write(p)
	} else {
		return len(p), nil
	}
}

func (lw LevelWriter) Write(p []byte) (n int, err error) {
	return lw.Writer.Write(p)
}

func checkVersionSupport(c grafana.Client, pmmURL, victoriaMetricsURL string) {
	checkUrls := []string{fmt.Sprintf("%s/api/v1/export/native", victoriaMetricsURL)}

	for _, v := range checkUrls {
		code, _, err := c.Get(v)
		if err == nil {
			if code == 404 {
				log.Error().Msg("There are 404 not-found errors occured when making test requests. Maybe PMM-server version is not supported!")
				log.Debug().Msgf("404 error by %s", v)
				break
			}
		} else {
			log.Fatal().Err(errors.Wrap(err, "failed to make test requests"))
		}
	}

	pmmVer, _, err := getPMMVersion(pmmURL, c)
	if err != nil {
		log.Fatal().Err(errors.Wrap(err, "failed to get PMM version"))
	}

	if pmmVer < minPMMServerVersion {
		log.Error().Msgf("Your PMM-server version %s is lower, than minimum required: %s!", pmmVer, minPMMServerVersion)
	}
}
