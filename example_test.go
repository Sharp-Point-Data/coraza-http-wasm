package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/http-wasm/http-wasm-host-go/handler"
	nethttp "github.com/http-wasm/http-wasm-host-go/handler/nethttp"
	wasm "github.com/http-wasm/http-wasm-host-go/handler/nethttp"
	"github.com/tetratelabs/wazero"
)

//go:embed build/coraza-http-wasm.wasm
var guest string

func exampleHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("Hello world, transaction not disrupted."))
}

func ExampleMain() {
	ctx := context.Background()

	h, err := wasm.NewMiddleware(
		ctx,
		[]byte(guest),
		handler.GuestConfig([]byte(`
		{
			"directives": [
				"SecRuleEngine On",
				"SecDebugLogLevel 9",
				"SecDebugLog /dev/stdout",
				"SecRule REQUEST_URI \"@rx .\" \"phase:1,deny,status:403,id:'1234'\""
			]
		}`),
		),
	)
	if err != nil {
		log.Fatalf("Failed to create middleware: %s", err.Error())
	}
	defer h.Close(ctx)

	w := h.NewHandler(ctx, http.HandlerFunc(exampleHandler))

	srvAddress := ":8080"
	srv := &http.Server{Addr: srvAddress, Handler: w}

	go func() {
		_ = srv.ListenAndServe()
	}()

	defer srv.Close()

	req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost%s?key=<alert>", srvAddress), nil)

	// The server binds asynchronously; retry briefly until it accepts connections.
	var res *http.Response
	for i := 0; i < 50; i++ {
		res, err = http.DefaultClient.Do(req)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		log.Fatalf("Failed to call the server: %s", err.Error()) // nolint: gocritic
	}

	fmt.Println(res.StatusCode)
	res.Body.Close()

	// Output: 403
}

func ExampleFS() {
	moduleConfig := wazero.
		NewModuleConfig().
		// Mount the directory as read-only at the root of the guest filesystem.
		WithFSConfig(wazero.NewFSConfig().WithReadOnlyDirMount("./testdata", "/"))

	mw, err := nethttp.NewMiddleware(context.Background(), []byte(guest),
		handler.ModuleConfig(moduleConfig),
		handler.GuestConfig([]byte("{\"directives\": [ \"Include ./directives.conf\", \"Include @crs-setup.conf.example\" ]}")),
	)
	if err == nil {
		mw.Close(context.Background())
	} else {
		fmt.Printf("failed to create middleware: %v\n", err)
	}

	// Output:
}
