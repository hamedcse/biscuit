package main

import "fmt"
import "sync"

// Cache of in-mmory objects. Main invariant: an object is in memory once so
// that threads see each other's updates.

var refcache_debug = false

type obj_t interface {
	evict()
	key() int
}

// The cache contains refcounted references to obj
type ref_t struct {
	sync.Mutex      // only there to initialize obj
	obj             obj_t
	refcnt          int
	key             int
	valid           bool
	s               string
	refnext		*ref_t
	refprev		*ref_t
}

type refcache_t struct {
	sync.Mutex
	size           int
	refs           map[int]*ref_t
	reflru         reflru_t
}

func make_refcache(size int) *refcache_t {
	ic := &refcache_t{}
	ic.size = size
	ic.refs = make(map[int]*ref_t, size)
	return ic
}

func (irc *refcache_t) print_live_refs() {
	fmt.Printf("refs %v\n", len(irc.refs))
	for _, r := range irc.refs {
		fmt.Printf("ref %v refcnt %v %v\n", r.key, r.refcnt, r.s)
	}
}

func (irc *refcache_t) _replace() obj_t {
	for ir := irc.reflru.tail; ir != nil; ir = ir.refprev {
		if ir.refcnt == 0 {
			if refcache_debug {
				fmt.Printf("_replace: victim %v %v\n", ir.key, ir.s)
			}
			delete(irc.refs, ir.key)
			irc.reflru.remove(ir)
			return ir.obj
		}
	}
	return nil
}

// returns a locked ref
func (irc *refcache_t) lookup(key int, s string) (*ref_t, err_t) {
	irc.Lock()
	
	ref, ok := irc.refs[key]
	if ok {
		ref.refcnt++
		if refcache_debug {
			fmt.Printf("ref hit %v %v %v\n", key, ref.refcnt, s)
		}
		irc.reflru.mkhead(ref)
		irc.Unlock()
		ref.Lock()
		return ref, 0
	}

	var victim obj_t
	if len(irc.refs) >= irc.size {
		victim = irc._replace()
		if victim == nil {
			fmt.Printf("refs in use %v limited %v\n", len(irc.refs), irc.size)
			irc.print_live_refs()
			return nil, -ENOMEM
		}
        }
 	
	ref = &ref_t{}
	ref.refcnt = 1
	ref.key = key
	ref.valid = false
	ref.s = s
	ref.Lock()
	
	irc.refs[key] = ref
	irc.reflru.mkhead(ref)
	
	if refcache_debug {
		fmt.Printf("ref miss %v cnt %v %s\n", key, ref.refcnt, s)
	}

	
	irc.Unlock()

	// release cache lock, now free victim
	if victim != nil {
		victim.evict()   // XXX in another thread?
	}

	return ref, 0
}


func (irc *refcache_t) refup(o obj_t, s string) {
	irc.Lock()
	defer irc.Unlock()
	
	ref, ok := irc.refs[o.key()]
	if !ok {
		panic("refup")
	}
	
	if refcache_debug {
		fmt.Printf("refdup %v cnt %v %s\n", o.key(), ref.refcnt, s)
	}

	ref.refcnt++
}

func (irc *refcache_t) refdown(o obj_t, s string) {
	irc.Lock()

	ref, ok := irc.refs[o.key()]
	if !ok {
		panic("refdown: key not present")
	}
	if o != ref.obj {
		panic("refdown: different obj")
	}

	if refcache_debug {
		fmt.Printf("refdown %v cnt %v %s\n", o.key(), ref.refcnt, s)
	}
	
	ref.refcnt--
	if ref.refcnt < 0 {
		panic("refdown")
	}

	// Uncomment to test eagerly evicting references
	// var victim obj_t
	// if ref.refcnt == 0 {
	// 	victim = irc._replace()   // should succeed
	// 	if victim == nil {
	// 		panic("refdown")
	// 	}
	// }

	defer irc.Unlock()

	// if victim != nil {
	// 	victim.evict()   // XXX in another thread?
	// }
}

// LRU list of references
type reflru_t struct {
	head	*ref_t
	tail	*ref_t
}

func (rl *reflru_t) mkhead(ir *ref_t) {
	if memtime {
		return
	}
	rl._mkhead(ir)
}

func (rl *reflru_t) _mkhead(ir *ref_t) {
	if rl.head == ir {
		return
	}
	rl._remove(ir)
	if rl.head != nil {
		rl.head.refprev = ir
	}
	ir.refnext = rl.head
	rl.head = ir
	if rl.tail == nil {
		rl.tail = ir
	}
}

func (rl *reflru_t) _remove(ir *ref_t) {
	if rl.tail == ir {
		rl.tail = ir.refprev
	}
	if rl.head == ir {
		rl.head = ir.refnext
	}
	if ir.refprev != nil {
		ir.refprev.refnext = ir.refnext
	}
	if ir.refnext != nil {
		ir.refnext.refprev = ir.refprev
	}
	ir.refprev, ir.refnext = nil, nil
}

func (rl *reflru_t) remove(ir *ref_t) {
	if memtime {
		return
	}
	rl._remove(ir)
}

