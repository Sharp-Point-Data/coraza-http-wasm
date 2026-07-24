package coreruleset

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	crstests "github.com/corazawaf/coraza-coreruleset/v4/tests"
	"github.com/coreruleset/go-ftw/v2/config"
	"github.com/coreruleset/go-ftw/v2/output"
	"github.com/coreruleset/go-ftw/v2/runner"
	"github.com/coreruleset/go-ftw/v2/test"
	"github.com/http-wasm/http-wasm-host-go/api"
	"github.com/http-wasm/http-wasm-host-go/handler"
	wasm "github.com/http-wasm/http-wasm-host-go/handler/nethttp"
	"github.com/mccutchen/go-httpbin/v2/httpbin"
	"github.com/rs/zerolog"
)

type logger struct {
	t *testing.T
	f *bufio.Writer
}

func (logger) IsEnabled(api.LogLevel) bool { return true }

func (l logger) Log(_ context.Context, _ api.LogLevel, msg string) {
	if strings.Contains(msg, "Coraza: Warning.") {
		l.f.Write([]byte(msg + "\n"))
		l.f.Flush()
		return
	}

	//l.t.Log(msg)
}

//go:embed build/coraza-http-wasm.wasm
var guestWasm []byte

func TestFTW(t *testing.T) {
	const directives = `
# Coraza config
Include @coraza.conf-recommended

# Custom Rules for testing and eventually overrides of the basic Coraza config
SecResponseBodyMimeType text/plain",
SecDefaultAction "phase:3,log,auditlog,pass"
SecDefaultAction "phase:4,log,auditlog,pass"
SecDefaultAction "phase:5,log,auditlog,pass"

# Rule 900005 from https://github.com/coreruleset/coreruleset/blob/v4.0/dev/tests/regression/README.md#requirements
SecAction "id:900005,\
  phase:1,\
  nolog,\
  pass,\
  ctl:ruleEngine=DetectionOnly,\
  ctl:ruleRemoveById=910000,\
  setvar:tx.blocking_paranoia_level=4,\
  setvar:tx.crs_validate_utf8_encoding=1,\
  setvar:tx.arg_name_length=100,\
  setvar:tx.arg_length=400,\
  setvar:tx.total_arg_length=64000,\
  setvar:tx.max_num_args=255,\
  setvar:tx.max_file_size=64100,\
  setvar:tx.combined_file_sizes=65535"

# Write the value from the X-CRS-Test header as a marker to the log
# Requests with X-CRS-Test header will not be matched by any rule. See https://github.com/coreruleset/go-ftw/pull/133
SecRule REQUEST_HEADERS:X-CRS-Test "@rx ^.*$" \
  "id:999999,\
  phase:1,\
  pass,\
  t:none,\
  log,\
  msg:'X-CRS-Test %{MATCHED_VAR}',\
  ctl:ruleRemoveById=1-999999"

# CRS basic config
Include @crs-setup.conf.example

# CRS rules (on top of which are applied the previously defined SecDefaultAction)
Include @owasp_crs/*.conf
`

	errorPath := filepath.Join(t.TempDir(), "error.log")
	errorFile, err := os.Create(errorPath)
	if err != nil {
		t.Fatalf("failed to create error log: %v", err)
	}

	mw, err := wasm.NewMiddleware(
		context.Background(),
		guestWasm,
		handler.GuestConfig([]byte(fmt.Sprintf("{\"directives\": [ %q ]}", directives))),
		handler.Logger(logger{t, bufio.NewWriter(errorFile)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Close(context.Background())

	var tests []*test.FTWTest
	err = doublestar.GlobWalk(crstests.FS, "**/*.yaml", func(path string, d os.DirEntry) error {
		yaml, err := fs.ReadFile(crstests.FS, path)
		if err != nil {
			return err
		}
		ftwt, err := test.GetTestFromYaml(yaml, path)
		if err != nil {
			return err
		}
		tests = append(tests, ftwt)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(tests) == 0 {
		t.Fatal("no tests found")
	}

	s := httptest.NewServer(mw.NewHandler(context.Background(), httpbin.New().Handler()))
	defer s.Close()

	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Warm up the middleware: the first request on a fresh connection pays a
	// full WAF+CRS re-initialization on a cold guest instance (~1s). Absorb it
	// here so go-ftw's first marker request doesn't race its read timeout.
	if res, err := http.Get(s.URL + "/status/200"); err != nil {
		t.Fatalf("warm-up request failed: %v", err)
	} else {
		res.Body.Close()
	}

	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())
	// TODO(anuraaga): Don't use global config for FTW for better support of programmatic.
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	cfg, err := config.NewConfigFromFile(".ftw.yml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.LogFile = errorPath
	cfg.TestOverride.Overrides.DestAddr = &host
	cfg.TestOverride.Overrides.Port = &port

	runnerConfig := config.NewRunnerConfiguration(cfg)
	runnerConfig.ShowTime = false
	// Debugging aid: FTW_INCLUDE=^942150 go test ... runs a subset of tests.
	if v := os.Getenv("FTW_INCLUDE"); v != "" {
		runnerConfig.Include = regexp.MustCompile(v)
	}
	// Generous: a cold guest instance re-initializes the full CRS before
	// answering its first request, which can exceed a few seconds on loaded
	// CI machines.
	runnerConfig.ReadTimeout = 30 * time.Second

	res, err := runner.Run(runnerConfig, tests, output.NewOutput("quiet", os.Stdout))
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("FTW stats: run=%d success=%d failed=%d skipped=%d totaltime=%v",
		res.Stats.Run, len(res.Stats.Success), len(res.Stats.Failed), len(res.Stats.Skipped), res.Stats.TotalTime)
	if len(res.Stats.Failed) > 0 {
		t.Errorf("failed tests: %v", res.Stats.Failed)
	}
}
