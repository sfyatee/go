package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"9fans.net/go/plan9"
	"9fans.net/go/plan9/client"
	"github.com/jacobsa/fuse"
	"github.com/jacobsa/fuse/fuseops"
	"github.com/jacobsa/fuse/fuseutil"
)

// usage matches the man page closely.
func usage() {
	fmt.Fprintln(os.Stderr, "usage: 9pfuse [-D] [-A attrtimeout] [-a aname] addr mtpt")
	os.Exit(2)
}

func main() {
	var (
		debugFlag = flag.Bool("D", false, "print debug info for FUSE ops")
		attrT     = flag.Float64("A", 1.0, "attribute cache timeout in seconds")
		anameFlag = flag.String("a", "", "9P attach name (currently advisory)")
	)
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 2 {
		usage()
	}

	addr := flag.Arg(0)
	mtpt := flag.Arg(1)

	if err := run(addr, mtpt, *anameFlag, *attrT, *debugFlag); err != nil {
		fmt.Fprintf(os.Stderr, "9pfuse: %v\n", err)
		os.Exit(1)
	}
}

func run(addr, mtpt, aname string, attrTimeout float64, debug bool) error {
	// For now we assume a TCP 9P server at host:port.
	// If you want Plan 9-style "tcp!host!port" or /srv-style mounts,
	// you can adjust this to call client.Dial/Attach or MountService.
	fsys, err := client.Mount("tcp", addr)
	if err != nil {
		return fmt.Errorf("mount 9p server %q: %w", addr, err)
	}
	defer fsys.Close()

	attrTTL := time.Duration(attrTimeout * float64(time.Second))
	fs := NewNinePFS(fsys, aname, attrTTL, debug)

	cfg := &fuse.MountConfig{
		FSName:    "9pfuse",
		OpContext: context.Background(),
	}
	if debug {
		cfg.DebugLogger = log.New(os.Stderr, "fuse: ", log.LstdFlags)
	}

	server := fuseutil.NewFileSystemServer(fs)

	mfs, err := fuse.Mount(mtpt, server, cfg)
	if err != nil {
		return fmt.Errorf("fuse mount %q: %w", mtpt, err)
	}
	defer func() {
		_ = fuse.Unmount(mfs.Dir())
	}()

	if debug {
		log.Printf("9pfuse: mounted %s on %s (attrTTL=%s)", addr, mtpt, attrTTL)
	}

	// Serve in the foreground.
	return mfs.Join(context.Background())
}

// ---------------
// 9P-backed FS
// ---------------

type inode struct {
	id     fuseops.InodeID
	parent fuseops.InodeID
	name   string
	path   string
	isDir  bool
}

type handle struct {
	id      fuseops.HandleID
	inode   *inode
	fid     *client.Fid
	isDir   bool
	entries []*plan9.Dir // for directory handles (cached listing)
}

type ninePFS struct {
	fuseutil.NotImplementedFileSystem

	fsys     *client.Fsys
	aname    string
	attrTTL  time.Duration
	entryTTL time.Duration
	debug    bool

	mu         sync.Mutex
	nextInode  fuseops.InodeID
	inodes     map[fuseops.InodeID]*inode
	pathInode  map[string]*inode
	nextHandle fuseops.HandleID
	handles    map[fuseops.HandleID]*handle
}

func NewNinePFS(fsys *client.Fsys, aname string, attrTTL time.Duration, debug bool) *ninePFS {
	fs := &ninePFS{
		fsys:       fsys,
		aname:      aname,
		attrTTL:    attrTTL,
		entryTTL:   attrTTL,
		debug:      debug,
		nextInode:  fuseops.RootInodeID + 1,
		inodes:     make(map[fuseops.InodeID]*inode),
		pathInode:  make(map[string]*inode),
		nextHandle: 1,
		handles:    make(map[fuseops.HandleID]*handle),
	}

	// Root inode, mapped to "/".
	root := &inode{
		id:     fuseops.RootInodeID,
		parent: fuseops.RootInodeID,
		name:   "",
		path:   "/",
		isDir:  true,
	}
	fs.inodes[root.id] = root
	fs.pathInode[root.path] = root

	return fs
}

func (fs *ninePFS) logf(format string, args ...interface{}) {
	if fs.debug {
		log.Printf(format, args...)
	}
}

func (fs *ninePFS) join(parentPath, name string) string {
	if parentPath == "/" {
		return "/" + name
	}
	return parentPath + "/" + name
}

func (fs *ninePFS) getInode(id fuseops.InodeID) *inode {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.inodes[id]
}

func (fs *ninePFS) getOrCreateInodeForPath(parent *inode, name string, isDir bool) *inode {
	full := fs.join(parent.path, name)

	fs.mu.Lock()
	defer fs.mu.Unlock()

	if in, ok := fs.pathInode[full]; ok {
		return in
	}

	id := fs.nextInode
	fs.nextInode++

	in := &inode{
		id:     id,
		parent: parent.id,
		name:   name,
		path:   full,
		isDir:  isDir,
	}
	fs.inodes[id] = in
	fs.pathInode[full] = in
	return in
}

func (fs *ninePFS) newHandle(in *inode, fid *client.Fid, isDir bool) fuseops.HandleID {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	hid := fs.nextHandle
	fs.nextHandle++
	fs.handles[hid] = &handle{
		id:    hid,
		inode: in,
		fid:   fid,
		isDir: isDir,
	}
	return hid
}

func (fs *ninePFS) getHandle(hid fuseops.HandleID) *handle {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.handles[hid]
}

func (fs *ninePFS) deleteHandle(hid fuseops.HandleID) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	delete(fs.handles, hid)
}

// Translate plan9.Dir into fuse attributes.
func (fs *ninePFS) dirToAttr(in *inode, d *plan9.Dir) fuseops.InodeAttributes {
	var mode os.FileMode
	if d.Mode&plan9.DMDIR != 0 {
		mode |= os.ModeDir
	}
	// We ignore other special bits and just set POSIX perms.
	mode |= os.FileMode(d.Mode & 0o777)

	uid := uint32(os.Getuid())
	gid := uint32(os.Getgid())

	// Plan 9 times are seconds since epoch.
	atime := time.Unix(int64(d.Atime), 0)
	mtime := time.Unix(int64(d.Mtime), 0)

	return fuseops.InodeAttributes{
		Size:   uint64(d.Length),
		Nlink:  1,
		Mode:   mode,
		Atime:  atime,
		Mtime:  mtime,
		Ctime:  mtime,
		Crtime: mtime,
		Uid:    uid,
		Gid:    gid,
		// InodeNumber isn't strictly needed; we can just reuse our ID.
		InodeNumber: uint64(in.id),
	}
}

func toFuseErr(err error) error {
	if err == nil {
		return nil
	}
	// Very simple mapping: not found -> ENOENT, everything else -> EIO.
	if os.IsNotExist(err) || strings.Contains(err.Error(), "does not exist") {
		return fuse.ENOENT
	}
	return fuse.EIO
}

// -------------------
// FS ops
// -------------------

func (fs *ninePFS) StatFS(ctx context.Context, op *fuseops.StatFSOp) error {
	// No real 9P statfs; just return some plausible numbers.
	op.BlockSize = 4096
	op.Blocks = 0
	op.BlocksFree = 0
	op.BlocksAvailable = 0
	op.Inodes = 0
	op.InodesFree = 0
	return nil
}

func (fs *ninePFS) GetInodeAttributes(ctx context.Context, op *fuseops.GetInodeAttributesOp) error {
	in := fs.getInode(op.Inode)
	if in == nil {
		return fuse.ENOENT
	}

	// Root may be special; Stat("/") should work though.
	name := strings.TrimPrefix(in.path, "/")
	if name == "" {
		name = "/"
	}

	fs.logf("GetInodeAttributes: %s", in.path)

	d, err := fs.fsys.Stat(name)
	if err != nil {
		return toFuseErr(err)
	}

	op.Attributes = fs.dirToAttr(in, d)
	return nil
}

func (fs *ninePFS) LookUpInode(ctx context.Context, op *fuseops.LookUpInodeOp) error {
	parent := fs.getInode(op.Parent)
	if parent == nil {
		return fuse.ENOENT
	}

	childPath := fs.join(parent.path, op.Name)
	name := strings.TrimPrefix(childPath, "/")
	if name == "" {
		name = "/"
	}

	fs.logf("LookUpInode: %s (parent=%s)", childPath, parent.path)

	d, err := fs.fsys.Stat(name)
	if err != nil {
		return toFuseErr(err)
	}

	isDir := d.Mode&plan9.DMDIR != 0
	child := fs.getOrCreateInodeForPath(parent, op.Name, isDir)

	op.Entry.Child = child.id
	op.Entry.Generation = 1
	op.Entry.Attributes = fs.dirToAttr(child, d)
	op.Entry.AttributesExpiration = time.Now().Add(fs.attrTTL)
	op.Entry.EntryExpiration = time.Now().Add(fs.entryTTL)
	return nil
}

func (fs *ninePFS) OpenFile(ctx context.Context, op *fuseops.OpenFileOp) error {
	in := fs.getInode(op.Inode)
	if in == nil {
		return fuse.ENOENT
	}

	name := strings.TrimPrefix(in.path, "/")
	if name == "" {
		name = "/"
	}

	fs.logf("OpenFile: %s", in.path)

	// For simplicity, always open read-write; the 9P server will enforce perms.
	fid, err := fs.fsys.Open(name, plan9.ORDWR)
	if err != nil {
		return toFuseErr(err)
	}

	op.Handle = fs.newHandle(in, fid, false)
	return nil
}

func (fs *ninePFS) ReadFile(ctx context.Context, op *fuseops.ReadFileOp) error {
	h := fs.getHandle(op.Handle)
	if h == nil || h.fid == nil {
		return fuse.EIO
	}

	fs.logf("ReadFile: %s offset=%d size=%d", h.inode.path, op.Offset, len(op.Dst))

	n, err := h.fid.ReadAt(op.Dst, op.Offset)
	if err != nil && err != os.EOF {
		return toFuseErr(err)
	}
	op.BytesRead = n
	return nil
}

func (fs *ninePFS) WriteFile(ctx context.Context, op *fuseops.WriteFileOp) error {
	h := fs.getHandle(op.Handle)
	if h == nil || h.fid == nil {
		return fuse.EIO
	}

	fs.logf("WriteFile: %s offset=%d size=%d", h.inode.path, op.Offset, len(op.Data))

	n, err := h.fid.WriteAt(op.Data, op.Offset)
	if err != nil {
		return toFuseErr(err)
	}
	op.BytesWritten = n
	return nil
}

func (fs *ninePFS) ReleaseFileHandle(ctx context.Context, op *fuseops.ReleaseFileHandleOp) error {
	h := fs.getHandle(op.Handle)
	if h == nil {
		return nil
	}

	fs.logf("ReleaseFileHandle: %s", h.inode.path)

	if h.fid != nil {
		_ = h.fid.Close()
	}
	fs.deleteHandle(op.Handle)
	return nil
}

func (fs *ninePFS) OpenDir(ctx context.Context, op *fuseops.OpenDirOp) error {
	in := fs.getInode(op.Inode)
	if in == nil {
		return fuse.ENOENT
	}
	if !in.isDir {
		return fuse.ENOTDIR
	}

	name := strings.TrimPrefix(in.path, "/")
	if name == "" {
		name = "/"
	}

	fs.logf("OpenDir: %s", in.path)

	// Open dir read-only and slurp all entries.
	fid, err := fs.fsys.Open(name, plan9.OREAD)
	if err != nil {
		return toFuseErr(err)
	}
	entries, err := fid.Dirreadall()
	_ = fid.Close()
	if err != nil {
		return toFuseErr(err)
	}

	hid := fs.newHandle(in, nil, true)
	h := fs.getHandle(hid)
	h.entries = entries

	op.Handle = hid
	return nil
}

func (fs *ninePFS) ReadDir(ctx context.Context, op *fuseops.ReadDirOp) error {
	h := fs.getHandle(op.Handle)
	if h == nil || !h.isDir {
		return fuse.EIO
	}

	fs.logf("ReadDir: %s offset=%d", h.inode.path, op.Offset)

	// We treat DirOffset as an index into h.entries.
	idx := int(op.Offset)
	if idx < 0 {
		idx = 0
	}

	var n int
	for i := idx; i < len(h.entries); i++ {
		d := h.entries[i]
		name := d.Name
		// Skip "." and ".." if the server returns them.
		if name == "." || name == ".." {
			continue
		}

		isDir := d.Mode&plan9.DMDIR != 0
		child := fs.getOrCreateInodeForPath(h.inode, name, isDir)

		var dt fuseutil.DirentType
		if isDir {
			dt = fuseutil.DT_Directory
		} else {
			dt = fuseutil.DT_File
		}

		dirent := fuseutil.Dirent{
			Offset: fuseops.DirOffset(i + 1),
			Inode:  child.id,
			Name:   name,
			Type:   dt,
		}

		written := fuseutil.WriteDirent(op.Dst[n:], dirent)
		if written == 0 {
			break
		}
		n += written
	}

	op.BytesRead = n
	return nil
}

func (fs *ninePFS) ReleaseDirHandle(ctx context.Context, op *fuseops.ReleaseDirHandleOp) error {
	h := fs.getHandle(op.Handle)
	if h == nil {
		return nil
	}

	fs.logf("ReleaseDirHandle: %s", h.inode.path)

	// Directory fid (if we ever keep it open) would be closed here.
	fs.deleteHandle(op.Handle)
	return nil
}

// MkDir creates a new directory.
func (fs *ninePFS) MkDir(ctx context.Context, op *fuseops.MkDirOp) error {
	parent := fs.getInode(op.Parent)
	if parent == nil {
		return fuse.ENOENT
	}

	full := fs.join(parent.path, op.Name)
	name := strings.TrimPrefix(full, "/")
	if name == "" {
		name = "/"
	}

	fs.logf("MkDir: %s mode=%#o", full, op.Mode.Perm())

	perm := plan9.Perm(op.Mode.Perm()) | plan9.DMDIR
	fid, err := fs.fsys.Create(name, plan9.OREAD, perm)
	if err != nil {
		return toFuseErr(err)
	}
	_ = fid.Close()

	// Stat to get attributes.
	d, err := fs.fsys.Stat(name)
	if err != nil {
		return toFuseErr(err)
	}

	child := fs.getOrCreateInodeForPath(parent, op.Name, true)
	op.Entry.Child = child.id
	op.Entry.Generation = 1
	op.Entry.Attributes = fs.dirToAttr(child, d)
	op.Entry.AttributesExpiration = time.Now().Add(fs.attrTTL)
	op.Entry.EntryExpiration = time.Now().Add(fs.entryTTL)

	return nil
}

// CreateFile creates and opens a new file.
func (fs *ninePFS) CreateFile(ctx context.Context, op *fuseops.CreateFileOp) error {
	parent := fs.getInode(op.Parent)
	if parent == nil {
		return fuse.ENOENT
	}

	full := fs.join(parent.path, op.Name)
	name := strings.TrimPrefix(full, "/")
	if name == "" {
		name = "/"
	}

	fs.logf("CreateFile: %s mode=%#o", full, op.Mode.Perm())

	perm := plan9.Perm(op.Mode.Perm())
	fid, err := fs.fsys.Create(name, plan9.ORDWR, perm)
	if err != nil {
		return toFuseErr(err)
	}

	// Stat to get attributes.
	d, err := fs.fsys.Stat(name)
	if err != nil {
		_ = fid.Close()
		return toFuseErr(err)
	}

	child := fs.getOrCreateInodeForPath(parent, op.Name, false)
	op.Entry.Child = child.id
	op.Entry.Generation = 1
	op.Entry.Attributes = fs.dirToAttr(child, d)
	op.Entry.AttributesExpiration = time.Now().Add(fs.attrTTL)
	op.Entry.EntryExpiration = time.Now().Add(fs.entryTTL)

	op.Handle = fs.newHandle(child, fid, false)
	return nil
}

// Unlink removes a file.
func (fs *ninePFS) Unlink(ctx context.Context, op *fuseops.UnlinkOp) error {
	parent := fs.getInode(op.Parent)
	if parent == nil {
		return fuse.ENOENT
	}

	full := fs.join(parent.path, op.Name)
	name := strings.TrimPrefix(full, "/")
	if name == "" {
		name = "/"
	}

	fs.logf("Unlink: %s", full)

	if err := fs.fsys.Remove(name); err != nil {
		return toFuseErr(err)
	}
	return nil
}

// RmDir removes a directory (9P Remove works for files or dirs).
func (fs *ninePFS) RmDir(ctx context.Context, op *fuseops.RmDirOp) error {
	parent := fs.getInode(op.Parent)
	if parent == nil {
		return fuse.ENOENT
	}

	full := fs.join(parent.path, op.Name)
	name := strings.TrimPrefix(full, "/")
	if name == "" {
		name = "/"
	}

	fs.logf("RmDir: %s", full)

	if err := fs.fsys.Remove(name); err != nil {
		return toFuseErr(err)
	}
	return nil
}

// Optional: very simple Forget implementation (we don't reuse inode IDs,
// so we can just drop references; worst case we leak a tiny bit).
func (fs *ninePFS) ForgetInode(ctx context.Context, op *fuseops.ForgetInodeOp) error {
	// Don't forget root.
	if op.Inode == fuseops.RootInodeID {
		return nil
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	if in, ok := fs.inodes[op.Inode]; ok {
		delete(fs.inodes, op.Inode)
		delete(fs.pathInode, in.path)
	}
	return nil
}

// Helpers for path cleanliness if you care about normalizing:
// currently not used, but kept here for easy tweaking.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	return "/" + strings.TrimPrefix(path.Clean(p), "/")
}
