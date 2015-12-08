package library

import (
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
	var conn = util.NewDBusConn()
	var address = "@session"
	var filter = "type='signal',sender='org.freedesktop.Notifications'"

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

			// filter - match rule
			newFilter, err := util.ParseString(msg, "Filter")
			if err != nil {
				b.Error(err)
				continue
			}

			// open connection
			connCreated := false
			if !conn.IsOpen() || address != newAddress {
				// close previous if need
				if conn.IsOpen() {
					// TODO: report possible errors?
					conn.RemoveAllMatchRules(true)
					conn.Close()
				}

				// try to open new
				err = conn.Open(newAddress)
				if err != nil {
					b.Error(err)
					continue
				}

				// watch signals
				err = conn.WatchSignals()
				if err != nil {
					b.Error(err)
					continue
				}

				address = newAddress // changed
				connCreated = true
			}

			// update filter
			conn.RemoveMatchRule(filter) // remove old
			if connCreated || filter != newFilter {
				err = conn.InsertMatchRule(newFilter)
				if err != nil {
					b.Error(err)
					continue
				}

				filter = newFilter // changed
			}

		// get parameters of the block
		case c := <-b.queryrule:
			c <- map[string]interface{}{
				"BusName": address,
				"Filter":  filter,
			}

		// got message from D-Bus
		case sig := <-conn.Signals:
			if sig != nil {
				// send it to the output
				b.out <- map[string]interface{}{
					"sender": sig.Sender,
					"path":   string(sig.Path),
					"name":   sig.Name,
					"body":   sig.Body,
				}
			}

		// quit the block
		case <-b.quit:
			// TODO: report possible errors?
			conn.RemoveAllMatchRules(true)
			conn.Close()
			return
		}
	}
}
