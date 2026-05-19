package main

import (
    "log"
    "crypto/sha1"
    "encoding/base64"
    "net"
    "net/http"
)

const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func main() {
    mux := http.NewServeMux()
    mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
        log.Printf("=== WS UPGRADE REQUEST ===")
        log.Printf("Method: %s", r.Method)
        log.Printf("URL: %s", r.URL.String())
        log.Printf("Headers:")
        for k, v := range r.Header {
            log.Printf("  %s: %s", k, v)
        }
        
        key := r.Header.Get("Sec-WebSocket-Key")
        log.Printf("Key from header: %q", key)
        log.Printf("Key bytes: %x", []byte(key))
        
        // Compute accept
        h := sha1.New()
        h.Write([]byte(key))
        h.Write([]byte(magicGUID))
        accept := base64.StdEncoding.EncodeToString(h.Sum(nil))
        log.Printf("Computed accept: %q", accept)
        
        hj, ok := w.(http.Hijacker)
        if !ok {
            http.Error(w, "no hijack", 500)
            return
        }
        netConn, bufrw, err := hj.Hijack()
        if err != nil {
            http.Error(w, err.Error(), 500)
            return
        }
        defer netConn.Close()
        
        resp := "HTTP/1.1 101 Switching Protocols\r\n" +
            "Upgrade: websocket\r\n" +
            "Connection: Upgrade\r\n" +
            "Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
        
        log.Printf("Sending response: %q", resp)
        
        n, err := bufrw.WriteString(resp)
        log.Printf("Wrote %d bytes, err=%v", n, err)
        
        err = bufrw.Flush()
        log.Printf("Flush err=%v", err)
        
        // Now try to read
        for {
            header := make([]byte, 2)
            _, err := bufrw.Read(header)
            if err != nil {
                log.Printf("Read error: %v", err)
                return
            }
            log.Printf("Got frame header: %x (opcode=%d)", header, header[0]&0x0F)
            
            masked := header[1]&0x80 != 0
            length := int64(header[1] & 0x7F)
            log.Printf("Masked=%v, length=%d", masked, length)
            
            if length == 126 {
                ext := make([]byte, 2)
                bufrw.Read(ext)
                length = int64(ext[0])<<8 | int64(ext[1])
            } else if length == 127 {
                ext := make([]byte, 8)
                bufrw.Read(ext)
                for i := 0; i < 8; i++ {
                    length = length<<8 | int64(ext[i])
                }
            }
            
            var mask [4]byte
            if masked {
                bufrw.Read(mask[:])
            }
            
            payload := make([]byte, length)
            bufrw.Read(payload)
            
            if masked {
                for i := range payload {
                    payload[i] ^= mask[i%4]
                }
            }
            log.Printf("Payload: %s", string(payload))
            
            // Echo back
            out := make([]byte, 2+len(payload))
            out[0] = 0x81 // FIN + text
            out[1] = byte(len(payload))
            copy(out[2:], payload)
            bufrw.Write(out)
            bufrw.Flush()
        }
    })
    
    // Serve a test page too
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/html")
        w.Write([]byte(`<!DOCTYPE html><html><body>
<script>
var ws = new WebSocket('ws://127.0.0.1:9976/ws');
ws.onopen = function() { document.body.innerHTML += '<p>WS OPEN</p>'; };
ws.onerror = function(e) { document.body.innerHTML += '<p>WS ERROR: ' + (e.message || '') + '</p>'; };
ws.onclose = function(e) { document.body.innerHTML += '<p>WS CLOSE: code=' + e.code + '</p>'; };
</script>
</body></html>`))
    })
    
    ln, err := net.Listen("tcp", "127.0.0.1:9976")
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Server on 127.0.0.1:9976")
    log.Fatal(http.Serve(ln, mux))
}
