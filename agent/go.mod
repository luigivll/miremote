module example.com/miremote/agent

go 1.24.7

replace gopkg.in/yaml.v3 => ./third_party/yaml.v3

replace golang.org/x/crypto => ./third_party/x-crypto

replace golang.org/x/net => ./third_party/x-net

replace golang.org/x/sys => ./third_party/x-sys

require (
	github.com/coder/websocket v1.8.15
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	github.com/pion/interceptor v0.1.37
	github.com/pion/rtcp v1.2.15
	github.com/pion/webrtc/v4 v4.0.7
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/pion/datachannel v1.5.10 // indirect
	github.com/pion/dtls/v3 v3.0.4 // indirect
	github.com/pion/ice/v4 v4.0.3 // indirect
	github.com/pion/logging v0.2.2 // indirect
	github.com/pion/mdns/v2 v2.0.7 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtp v1.8.10 // indirect
	github.com/pion/sctp v1.8.35 // indirect
	github.com/pion/sdp/v3 v3.0.9 // indirect
	github.com/pion/srtp/v3 v3.0.4 // indirect
	github.com/pion/stun/v3 v3.0.0 // indirect
	github.com/pion/transport/v3 v3.0.7 // indirect
	github.com/pion/turn/v4 v4.0.0 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/crypto v0.29.0 // indirect
	golang.org/x/net v0.31.0 // indirect
	golang.org/x/sys v0.27.0 // indirect
)
