package library

import (
	"github.com/godbus/dbus"
	"github.com/nytlabs/streamtools/st/blocks" // blocks
	"github.com/nytlabs/streamtools/st/util"
)

// specify those channels we're going to use to communicate with streamtools
type ToDBus struct {
	blocks.Block
	queryrule chan blocks.MsgChan
	inrule    blocks.MsgChan
	in        blocks.MsgChan
	quit      blocks.MsgChan
}

// we need to build a simple factory so that streamtools can make new blocks of this kind
func NewToDBus() blocks.BlockInterface {
	return &ToDBus{}
}

// Setup is called once before running the block. We build up the channels and specify what kind of block this is.
func (b *ToDBus) Setup() {
	b.Kind = "D-Bus I/O"
	b.Desc = "writes out messages to D-Bus"
	b.inrule = b.InRoute("rule")
	b.queryrule = b.QueryRoute("rule")
	b.quit = b.Quit()
	b.in = b.InRoute("in")
}

// Run is the block's main loop. Here we listen on the different channels we set up.
func (b *ToDBus) Run() {
	var conn = util.NewDBusConn()
	var address = "@system"
	var dest = "org.freedesktop.Notifications"
	var path = "/org/freedesktop/Notifications"
	var name = "org.freedesktop.Notifications.Notify"
	var signature = dbus.ParseSignatureMust("susssasa{sv}i")

	for {
		select {
		// set parameters of the block
		case msg := <-b.inrule:
			// address - bus name
			newAddress, err := util.ParseString(msg, "BusName")
			if err != nil {
				b.Error(err)
				continue
			}

			// destination
			dest, err = util.ParseString(msg, "Destination")
			if err != nil {
				b.Error(err)
				continue
			}

			// path
			path, err = util.ParseString(msg, "Path")
			if err != nil {
				b.Error(err)
				continue
			}

			// name = interface + method
			name, err = util.ParseString(msg, "MethodName")
			if err != nil {
				b.Error(err)
				continue
			}

			// signature
			sig, err := util.ParseString(msg, "Signature")
			if err != nil {
				b.Error(err)
				continue
			}
			signature, err = dbus.ParseSignature(sig)
			if err != nil {
				b.Error(err)
				continue
			}

			// open connection
			if !conn.IsOpen() || address != newAddress {
				// close previous if need
				if conn.IsOpen() {
					// TODO: report possible errors?
					conn.Close()
				}

				// try to open new
				err = conn.Open(newAddress)
				if err != nil {
					b.Error(err)
					continue
				}

				address = newAddress // changed
			}

		// get parameters of the block
		case c := <-b.queryrule:
			c <- map[string]interface{}{
				"BusName":     address,
				"Destination": dest,
				"Path":        path,
				"MethodName":  name,
				"Signature":   signature.String(),
			}

		// got new message
		case msg := <-b.in:
			if conn.IsOpen() {
				args, err := util.ParseArray(msg, "args")
				if err != nil {
					b.Error(err)
					continue
				}
				args, err = util.DBusConv(signature, args...)
				if err != nil {
					b.Error(err)
					continue
				}

				obj := conn.Object(dest, path)
				//log.Printf("calling D-BUS method: %+v", args)
				call := obj.Call(name, 0, args...)
				if call.Err != nil {
					b.Error(call.Err)
					continue
				}
			}

		// quit the block
		case <-b.quit:
			// TODO: report possible errors?
			conn.Close()
			return
		}
	}
}
