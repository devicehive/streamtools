package library

import (
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
	var conn = newDBusConn()
	var address = "@system"
	var filter = "type='signal',sender='org.freedesktop.DBus'"

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
			if !conn.isOpen() || address != newAddress {
				// close previous if need
				if conn.isOpen() {
					// TODO: report possible errors?
					conn.removeAllMatchRules(true)
					conn.close()
				}

				// try to open new
				err = conn.open(newAddress)
				if err != nil {
					b.Error(err)
					continue
				}

				address = newAddress // changed
				connCreated = true
			}

			// update filter
			conn.removeMatchRule(filter) // remove old
			if connCreated || filter != newFilter {
				err = conn.insertMatchRule(newFilter)
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
		case sig := <-conn.signals:
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
			conn.removeAllMatchRules(true)
			conn.close()
			return
		}
	}
}

// D-Bus helper connection
type dbusConn struct {
	dbus      *dbus.Conn
	exclusive bool

	matchRules map[string]bool
	signals    chan *dbus.Signal
}

// create new D-Bus helper connection
func newDBusConn() (conn *dbusConn) {
	conn = &dbusConn{}
	conn.matchRules = map[string]bool{}
	conn.signals = make(chan *dbus.Signal, 1024)
	return
}

// isOpen() checks if D-Bus connection exists
func (conn *dbusConn) isOpen() bool {
	return conn.dbus != nil
}

// open() tries to open D-Bus connection
func (conn *dbusConn) open(address string) (err error) {
	switch strings.ToLower(address) {
	case "@system", "system":
		conn.dbus, err = dbus.SystemBus()
		conn.exclusive = false

	case "@session", "session":
		conn.dbus, err = dbus.SessionBus()
		conn.exclusive = false

	default: // dial by address
		conn.dbus, err = dbus.Dial(address)
		conn.exclusive = true
		if err != nil {
			return
		}

		// authenticate
		err = conn.dbus.Auth(nil)
		if err != nil {
			conn.dbus.Close()
			conn.dbus = nil
			return
		}

		// initialize
		err = conn.dbus.Hello()
		if err != nil {
			conn.dbus.Close()
			conn.dbus = nil
			return
		}
	}

	// watch for signals
	if conn.dbus != nil && err == nil {
		conn.dbus.Signal(conn.signals)
	}

	return
}

// close() closes the D-Bus connection
func (conn *dbusConn) close() (err error) {
	if conn.dbus != nil {
		// TODO: delete signals from D-Bus connection!?
		if conn.exclusive {
			err = conn.dbus.Close()
		}
		conn.dbus = nil
	}

	return
}

// insertMatchRule() adds match rule to the D-Bus object
func (conn *dbusConn) insertMatchRule(rule string) (err error) {
	// duplicates are not allowed
	if conn.dbus != nil && !conn.matchRules[rule] {
		call := conn.dbus.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, rule)
		err = call.Err
		if err == nil {
			// add rule to the matchRules set
			conn.matchRules[rule] = true
		}
	}

	return
}

// removeMatchRule() removes match rule from the D-Bus object
func (conn *dbusConn) removeMatchRule(rule string) (err error) {
	if conn.dbus != nil && conn.matchRules[rule] {
		call := conn.dbus.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, rule)
		err = call.Err
		if err == nil {
			// remove rule from the matchRules set
			delete(conn.matchRules, rule)
		}
	}

	return
}

// removeAllMatchRules() removes all installed match rules from the D-Bus object
func (conn *dbusConn) removeAllMatchRules(force bool) (err error) {
	for rule, _ := range conn.matchRules {
		err = conn.removeMatchRule(rule)
		if err != nil && !force {
			break
		}
	}

	if force {
		// clear all rules anyway
		conn.matchRules = map[string]bool{}
	}

	return
}
