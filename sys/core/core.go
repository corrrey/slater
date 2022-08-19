package core

import (
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("slater:core")

type Core struct {
	store    datastore
	host     node
	devices  []string
	sessions map[string]session
	Input    chan any
	Output   chan any
}

func Start() Core {
	logging.SetLogLevel("slater:core", "debug")

	core := Core{
		devices:  make([]string, 0),
		sessions: make(map[string]session),
		Input:    make(chan any),
		Output:   make(chan any),
	}

	go core.Run()

	return core
}

func (core *Core) Run() {
	for {
		select {
		case input := <-core.Input:
			switch input.(type) {
			case InputUISessionStart:
				msg := input.(InputUISessionStart)
				session := msg.Session
				go core.connect(session)

			case InputUISessionResume:
				msg := input.(InputUISessionResume)
				session := msg.Session
				go core.resumeSession(session)

			case InputUIMessage:
				msg := input.(InputUIMessage)
				session := msg.Session
				message := msg.Message
				go core.handleUIMessage(session, message)
			}
		}
	}
}

type InputUISessionStart struct {
	Session string
}

type InputUISessionResume struct {
	Session string
}

type InputUIMessage struct {
	Session string
	Message message
}

type OutputUIMessage struct {
	Session string
	Message message
}

func (core *Core) connect(sid string) {
	session := newSession(sid)
	core.sessions[sid] = session

	core.sendSessionID(sid)

	setup := "setup"
	core.sendAddSlate(sid, setup)

	feed := session.view.slates[setup]

	feed.on(ALL, func(_ slate, msg message, _ int) {
		core.sendMessage(sid, msg)
	})

	store, host := runSetup(core, feed)
	core.store = store
	core.host = host

	go core.handleNet(host)
}

func (core *Core) resumeSession(sid string) {
	session, there := core.sessions[sid]

	if !there {
		core.connect(sid)
		return
	}

	setup := "setup"
	core.sendAddSlate(sid, setup)

	s := session.view.slates[setup]

	// send everything on slate setup TODO PAGINATION!
	msgs, err := s.getRange(0, -1)
	if err != nil {
		log.Debug(err)
	}
	for _, msg := range msgs {
		core.sendMessage(sid, msg)
	}
}

func (core *Core) handleUIMessage(sid string, msg message) {
	log.Debugf("message from session %v:\n%v", sid, msg)

	session, there := core.sessions[sid]

	if !there {
		log.Debugf("discarded uiMessage from uninitialized session!")
		return
	}

	kind := msg["kind"]
	switch kind {
	case "msg":
		m := msg["msg"].(map[string]any)

		m["sent"] = timestamp()

		slateName := m["slate"].(string)

		slate, there := session.view.slates[slateName]
		if there {
			slate.write(m)
		} else {
			log.Debugf("failed to write to slate %s", slate)
		}
	}
}

func (core *Core) handleNet(peer node) {
	for {
		msg := <-peer.output

		msg["recv"] = timestamp()

		slateNameEntry, exists := msg["slate"]
		if !exists {
			continue // ignore. TODO consider bad behavior
		}

		slateName, ok := slateNameEntry.(string)
		if !ok {
			continue // ignore. TODO consider bad behavior
		}

		kindEntry, exists := msg["kind"]
		if !exists {
			continue // ignore
		}

		kind, ok := kindEntry.(string)
		if !ok {
			continue // ignore
		}

		if kind == "signet" {
			log.Debug("replying with signet")
			peer.send(peer.discoveryKey, message{
				"slate":  "setup",
				"kind":   "signet",
				"signet": peer.signet,
			})
		}

		for _, session := range core.sessions {
			slate, there := session.view.slates[slateName]
			if there {
				slate.write(msg)
				break
			} else {
				continue
			}
		}
	}
}

func (core *Core) sendMessage(sid string, msg message) {
	cmd := message{"kind": "msg", "msg": msg}
	core.Output <- OutputUIMessage{sid, cmd}
}

func (core *Core) sendText(sid string, slate string, text string) {
	core.sendTextFrom(sid, slate, "system", text)
}

func (core *Core) sendTextFrom(sid string, slate string, author string, text string) {
	msg := message{
		"slate":  slate,
		"author": author,
		"kind":   "text",
		"body":   text,
		"sent":   timestamp(),
	}
	cmd := message{"kind": "msg", "msg": msg}
	core.Output <- OutputUIMessage{sid, cmd}
}

func (core *Core) sendWeb(sid string, slate string, title string, url string) {
	core.sendWebFrom(sid, slate, "system", title, url)
}

func (core *Core) sendWebFrom(sid string, slate string, author string, title string, url string) {
	msg := message{
		"slate":  slate,
		"author": author,
		"kind":   "web",
		"title":  title,
		"body":   url,
		"sent":   timestamp(),
	}
	cmd := message{"kind": "msg", "msg": msg}
	core.Output <- OutputUIMessage{sid, cmd}
}

func (core *Core) sendSessionID(sid string) {
	cmd := message{"kind": "session", "session": sid}
	core.Output <- OutputUIMessage{sid, cmd}
}

func (core *Core) sendAddSlate(sid string, id string) {
	cmd := message{"kind": "slate", "session": sid, "slate": id}
	core.Output <- OutputUIMessage{sid, cmd}
}

func (core *Core) sendPage(sid string, page []message) {
	cmd := message{"kind": "page", "session": sid, "page": page}
	core.Output <- OutputUIMessage{sid, cmd}
}