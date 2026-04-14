package tunnel

import (
	"sort"
	"sync/atomic"
	"time"
)

type ManagedSessionSnapshot struct {
	ID                string     `json:"id"`
	Engine            string     `json:"engine"`
	Protocol          string     `json:"protocol"`
	ClientName        string     `json:"clientName"`
	RouteName         string     `json:"routeName"`
	PublicPort        int        `json:"publicPort"`
	TargetAddr        string     `json:"targetAddr"`
	SourceAddr        string     `json:"sourceAddr"`
	OpenedAt          time.Time  `json:"openedAt"`
	LastActivityAt    time.Time  `json:"lastActivityAt"`
	ClosedAt          *time.Time `json:"closedAt,omitempty"`
	BytesFromPublic   int64      `json:"bytesFromPublic"`
	BytesToPublic     int64      `json:"bytesToPublic"`
	PacketsFromPublic int64      `json:"packetsFromPublic"`
	PacketsToPublic   int64      `json:"packetsToPublic"`
}

type managedSessionRecord struct {
	id         string
	engine     string
	protocol   string
	clientName string
	routeName  string
	publicPort int
	targetAddr string
	sourceAddr string
	openedAt   time.Time

	lastActivityAt    atomic.Int64
	bytesFromPublic   atomic.Int64
	bytesToPublic     atomic.Int64
	packetsFromPublic atomic.Int64
	packetsToPublic   atomic.Int64
	closedAt          atomic.Int64
}

func newManagedSessionRecord(id, engine, protocol, clientName, routeName string, publicPort int, targetAddr, sourceAddr string) *managedSessionRecord {
	now := time.Now()
	record := &managedSessionRecord{
		id:         id,
		engine:     normalizeTunnelEngine(engine),
		protocol:   normalizeManagedRouteProtocol(protocol),
		clientName: clientName,
		routeName:  routeName,
		publicPort: publicPort,
		targetAddr: targetAddr,
		sourceAddr: sourceAddr,
		openedAt:   now,
	}
	record.lastActivityAt.Store(now.UnixNano())
	return record
}

func (r *managedSessionRecord) addBytesFromPublic(n int) {
	r.bytesFromPublic.Add(int64(n))
	r.touch()
}

func (r *managedSessionRecord) addBytesToPublic(n int) {
	r.bytesToPublic.Add(int64(n))
	r.touch()
}

func (r *managedSessionRecord) addPacketFromPublic(n int) {
	r.packetsFromPublic.Add(1)
	r.bytesFromPublic.Add(int64(n))
	r.touch()
}

func (r *managedSessionRecord) addPacketToPublic(n int) {
	r.packetsToPublic.Add(1)
	r.bytesToPublic.Add(int64(n))
	r.touch()
}

func (r *managedSessionRecord) touch() {
	r.lastActivityAt.Store(time.Now().UnixNano())
}

func (r *managedSessionRecord) snapshot() ManagedSessionSnapshot {
	lastActivityAt := time.Unix(0, r.lastActivityAt.Load())
	var closedAt *time.Time
	if closedAtUnix := r.closedAt.Load(); closedAtUnix > 0 {
		closed := time.Unix(0, closedAtUnix)
		closedAt = &closed
	}
	return ManagedSessionSnapshot{
		ID:                r.id,
		Engine:            r.engine,
		Protocol:          r.protocol,
		ClientName:        r.clientName,
		RouteName:         r.routeName,
		PublicPort:        r.publicPort,
		TargetAddr:        r.targetAddr,
		SourceAddr:        r.sourceAddr,
		OpenedAt:          r.openedAt,
		LastActivityAt:    lastActivityAt,
		ClosedAt:          closedAt,
		BytesFromPublic:   r.bytesFromPublic.Load(),
		BytesToPublic:     r.bytesToPublic.Load(),
		PacketsFromPublic: r.packetsFromPublic.Load(),
		PacketsToPublic:   r.packetsToPublic.Load(),
	}
}

func (s *ManagedServer) trackActiveSession(record *managedSessionRecord) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	s.sessions[record.id] = record
}

func (s *ManagedServer) untrackActiveSession(sessionID string) {
	s.sessionsMu.Lock()
	delete(s.sessions, sessionID)
	s.sessionsMu.Unlock()
}

func (s *ManagedServer) ListActiveSessions() []ManagedSessionSnapshot {
	s.sessionsMu.RLock()
	snapshots := make([]ManagedSessionSnapshot, 0, len(s.sessions))
	for _, record := range s.sessions {
		snapshots = append(snapshots, record.snapshot())
	}
	s.sessionsMu.RUnlock()

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].OpenedAt.After(snapshots[j].OpenedAt)
	})
	return snapshots
}

func (s *ManagedServer) ActiveSessionCount() int {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	return len(s.sessions)
}
