//go:build !nogui && webviewprobe

package app

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"

	"github.com/egoist/mygo"
	"github.com/fishioon/aicat/internal/webviewprobe"
)

func RunWebViewProbe(o Options, previewAddr string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := start(ctx, o)
	if err != nil {
		return err
	}
	defer r.close()
	host := webviewprobe.New(r.traffic, r.capture.SetRecording, r.initialTrafficEnabled)
	mygo.SetFrontend(webviewprobe.Frontend())
	mygo.Protocol.Handle("probeapi", host)
	mygo.App.SetName("aicat WebView probe")
	var server *http.Server
	if previewAddr != "" {
		listener, err := net.Listen("tcp", previewAddr)
		if err != nil {
			return err
		}
		server = &http.Server{Handler: host.Preview()}
		go func() {
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("preview: %v", err)
			}
		}()
		defer webviewprobe.ClosePreview(context.Background(), server)
		log.Printf("WebView probe preview http://%s", listener.Addr())
	}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "aicat · WebView probe", Width: 1180, Height: 900, MinWidth: 900, MinHeight: 600,
			URL: "/", TitleBarStyle: mygo.TitleBarHidden, BackgroundColor: "#ffffff",
		})
		win.OnClose(func(*mygo.CloseEvent) { mygo.App.Quit() })
	})
	return mygo.App.Run()
}
