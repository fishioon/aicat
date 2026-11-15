package gateway

import (
	"net/http"

	"github.com/lqqyt2423/go-mitmproxy/proxy"
)

type httpTransport struct{ proxy.BaseAddon }

func (*httpTransport) Requestheaders(flow *proxy.Flow) {
	if !flow.ConnContext.ClientConn.Tls && flow.Request.Method != http.MethodConnect {
		flow.UseSeparateClient = true
	}
}
