// Command oldproto answers the handshake with a protocol version nobody
// speaks, in either transport, and then waits.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"os"
	"time"

	"github.com/Shougo/goda.vim/internal/msgpack"
)

func main() {
	transport := flag.String("transport", "", "")
	flag.Parse()
	hello := map[string]any{"protocol": int64(99), "version": "x", "plugins": map[string]any{}}
	switch *transport {
	case "vim":
		var req []json.RawMessage
		if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
			return
		}
		var id int64
		_ = json.Unmarshal(req[0], &id)
		b, _ := json.Marshal([]any{id, map[string]any{"result": hello}})
		os.Stdout.Write(append(b, '\n'))
	case "nvim":
		v, err := msgpack.NewDecoder(bufio.NewReader(os.Stdin)).Decode()
		if err != nil {
			return
		}
		req, _ := v.([]any)
		b, _ := msgpack.Encode([]any{int64(1), req[1], nil, hello})
		os.Stdout.Write(b)
	}
	time.Sleep(3 * time.Second)
}
