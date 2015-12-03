package library

import (
	//	"encoding/json"
	"strings"

	"github.com/godbus/dbus"
	"github.com/nytlabs/streamtools/st/blocks" // blocks
	"github.com/nytlabs/streamtools/st/util"
)

// specify those channels we're going to use to communicate with streamtools
type FromDBus struct {
	blocks.Block
	queryrule chan blocks.MsgChan
	inrule    blocks.MsgChan
	out       blocks.MsgChan
	quit      blocks.MsgChan
}

// we need to build a simple factory so that streamtools can make new blocks of this kind
func NewFromDBus() blocks.BlockInterface {
	return &FromDBus{}
}

// Setup is called once before running the block. We build up the channels and specify what kind of block this is.
func (b *FromDBus) Setup() {
	b.Kind = "D-Bus I/O"
	b.Desc = "reads in signals/messages from D-Bus"
	b.inrule = b.InRoute("rule")
	b.queryrule = b.QueryRoute("rule")
	b.quit = b.Quit()
	b.out = b.Broadcast()
}

// Run is the block's main loop. Here we listen on the different channels we set up.
func (b *FromDBus) Run() {
	var conn *dbus.Conn
	var connNeedClose bool
	var sigch chan *dbus.Signal
	var address = "system"
	var filter = "type='signal',sender='*'"
	var err error

	for {
		select {
		// set parameters of the block
		case msgI := <-b.inrule:
			// address - bus name
			address, err = util.ParseString(msgI, "BusName")
			if err != nil {
				b.Error(err)
				continue
			}

			// match filter
			filter, err = util.ParseString(msgI, "Filter")
			if err != nil {
				b.Error(err)
				continue
			}

			// open connection
			if conn != nil && connNeedClose {
				conn.Close() // TODO: report possible errors?
				conn = nil
			}
			conn, connNeedClose, err = openConn(address, filter)
			if err != nil {
				b.Error(err)
				continue
			}
			sigch = make(chan *dbus.Signal, 1024)
			conn.Signal(sigch)

		// get parameters of the block
		case c := <-b.queryrule:
			c <- map[string]interface{}{
				"BusName": address,
				"Filter":  filter,
			}

		// got message from D-Bus
		case sig := <-sigch:
			if sig == nil {
				// ignore
				continue
			}
			b.out <- map[string]interface{}{
				"sender": sig.Sender,
				"path":   string(sig.Path),
				"name":   sig.Name,
				"body":   sig.Body,
			}

		// quit the block
		case <-b.quit:
			// quit the block
			if conn != nil && connNeedClose {
				conn.Close() // TODO: report possible errors?
				conn = nil
			}
			return
		}
	}
}

// openConn tries to open bus and adds corresponding filter
func openConn(address, filter string) (conn *dbus.Conn, needClose bool, err error) {
	// open corresponding bus
	switch strings.ToLower(address) {
	case "system":
		conn, err = dbus.SystemBus()
		if err != nil {
			return
		}

	case "session":
		conn, err = dbus.SessionBus()
		if err != nil {
			return
		}

	default: // dial by address
		conn, err = dbus.Dial(address)
		if err != nil {
			return
		}

		// authenticate
		err = conn.Auth(nil)
		if err != nil {
			conn.Close()
			conn = nil
			return
		}

		// initialize
		err = conn.Hello()
		if err != nil {
			conn.Close()
			conn = nil
			return
		}

		// need to close custom connection
		needClose = true
	}

	// add match
	call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, filter)
	err = call.Err
	if err != nil {
		if needClose {
			conn.Close()
		}
		conn = nil
		return
	}

	return // OK
}
