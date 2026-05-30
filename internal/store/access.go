package store

import "github.com/google/uuid"

type Access int

const (
	NoAccess Access = iota
	ReadAccess
	WriteAccess
)

// String renders an Access level as its wire/spec name ("none"|"read"|"write").
func (a Access) String() string {
	switch a {
	case WriteAccess:
		return "write"
	case ReadAccess:
		return "read"
	default:
		return "none"
	}
}

// permLevel maps a stored permission string to an Access level. It fails closed:
// any value other than the known enum values yields NoAccess, so a corrupted or
// unexpected permission can never silently grant access.
func permLevel(p string) Access {
	switch p {
	case "write":
		return WriteAccess
	case "read":
		return ReadAccess
	default:
		return NoAccess
	}
}

// projectFacts is the minimal authorization-relevant view of a project, decoupled
// from ent so the rule is trivially testable.
type projectFacts struct {
	Visibility  string
	OwnerID     uuid.UUID
	UserShares  map[uuid.UUID]string // userID -> "read"|"write"
	GroupShares map[string]string    // group  -> "read"|"write"
}

// effectiveAccess implements spec §4: highest matching grant wins.
func effectiveAccess(p projectFacts, id Identity) Access {
	if id.IsAdmin || p.OwnerID == id.UserID {
		return WriteAccess
	}
	if p.Visibility == "org" {
		return WriteAccess // org membership grants read+write; nothing can exceed it
	}
	best := NoAccess
	if lvl, ok := p.UserShares[id.UserID]; ok {
		if l := permLevel(lvl); l > best {
			best = l
		}
	}
	for _, g := range id.Groups {
		if lvl, ok := p.GroupShares[g]; ok {
			if l := permLevel(lvl); l > best {
				best = l
			}
		}
	}
	return best
}
