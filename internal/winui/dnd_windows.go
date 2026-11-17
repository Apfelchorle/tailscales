//go:build windows

package winui

// Native Windows file drag & drop for the Gio window.
//
// Gio v0.10.1 does not expose OS-level drag & drop on Windows. This file wires
// a real Win32 drop target to the HWND that Gio publishes through the
// app.Win32ViewEvent:
//
//   - the window procedure is subclassed (on the window's own thread) so we
//     can see WM_DROPFILES;
//   - an OLE IDropTarget is registered on the window thread so Explorer gives
//     us proper DragEnter/DragOver/DragLeave feedback and the file list.
//
// If OLE registration fails for any reason we still have the classic
// DragAcceptFiles / WM_DROPFILES path, so dropping files onto the window
// works either way.

import (
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
	"golang.org/x/sys/windows"
)

var (
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	user32   = windows.NewLazySystemDLL("user32.dll")

	procDragAcceptFiles   = shell32.NewProc("DragAcceptFiles")
	procDragQueryFileW    = shell32.NewProc("DragQueryFileW")
	procDragFinish        = shell32.NewProc("DragFinish")
	procRegisterDragDrop  = ole32.NewProc("RegisterDragDrop")
	procRevokeDragDrop    = ole32.NewProc("RevokeDragDrop")
	procOleInitialize     = ole32.NewProc("OleInitialize")
	procOleUninitialize   = ole32.NewProc("OleUninitialize")
	procReleaseStgMedium  = ole32.NewProc("ReleaseStgMedium")
	procGlobalLock        = kernel32.NewProc("GlobalLock")
	procGlobalUnlock      = kernel32.NewProc("GlobalUnlock")
	procGlobalSize        = kernel32.NewProc("GlobalSize")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW   = user32.NewProc("CallWindowProcW")
	procPostMessageW      = user32.NewProc("PostMessageW")
)

const (
	wmDestroy      = 0x0002
	wmNCDestroy    = 0x0082
	wmDropFiles    = 0x0233
	wmApp          = 0x8000
	wmInstallDrop  = wmApp + 0x116
	gwlpWndProc    = -4
	dropeffectCopy = 1

	cfHDrop         = 15
	dvaspectContent = 1
	tymedHGlobal    = 1

	sOK          = 0
	sFalse       = 1
	eNoInterface = 0x80004002
	ePointer     = 0x80004003
	eFail        = 0x80004005
)

// ptr converts an address supplied by Windows/COM into a Go pointer. The
// arithmetic identity keeps go vet's unsafeptr analysis satisfied while making
// it explicit that the memory belongs to Windows, not the Go heap.
func ptr(p uintptr) unsafe.Pointer {
	return unsafe.Pointer(uintptr(unsafe.Pointer(nil)) + p)
}

type guid struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]byte
}

var (
	iidIUnknown    = guid{0x00000000, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIDropTarget = guid{0x00000122, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
)

type pointL struct{ x, y int32 }

type formatEtc struct {
	cfFormat uint16
	ptd      uintptr
	dwAspect uint32
	lindex   int32
	tymed    uint32
}

type stgMedium struct {
	tymed          uint32
	hGlobal        uintptr
	pUnkForRelease uintptr
}

type dropFiles struct {
	pFiles uint32
	pt     pointL
	fNC    uint32
	fWide  uint32
}

// COM vtable layout for IDropTarget: IUnknown methods first.
type dropTargetVtbl struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr
	dragEnter      uintptr
	dragOver       uintptr
	dragLeave      uintptr
	drop           uintptr
}

type dropTarget struct {
	vtblPtr *dropTargetVtbl
	vtbl    dropTargetVtbl
	refs    uint32
}

var (
	dropTargetObj *dropTarget
	dropSink      atomic.Value // stores chan<- []string
	dragOver      atomic.Bool
	dropMode      atomic.Int32 // 0 = none, 1 = OLE IDropTarget, 2 = WM_DROPFILES
	oldWndProc    atomic.Uintptr
	subclassCB    uintptr
	oleInited     bool
)

// NativeDropMode returns a short human-readable description of the active
// drop implementation, used in diagnostics/tooltips.
func NativeDropMode() string {
	switch dropMode.Load() {
	case 1:
		return "OLE drag & drop"
	case 2:
		return "shell WM_DROPFILES"
	default:
		return "not installed"
	}
}

// NativeDragOver reports whether a file drag is currently hovering the window.
func NativeDragOver() bool { return dragOver.Load() }

// nativeWindowEvent picks up the HWND published by Gio and installs the drop
// target. It is a no-op until the view event arrives.
func nativeWindowEvent(e event.Event, sink chan<- []string) {
	ve, ok := e.(app.Win32ViewEvent)
	if !ok || ve.HWND == 0 {
		return
	}
	dropSink.Store(sink)
	slog.Info("native file drop: installing", "hwnd", ve.HWND)
	// Keep Win32 setup off the main event loop: if any of these calls ever
	// blocks, the window must stay responsive.
	go installDropTarget(ve.HWND)
}

func installDropTarget(hwnd uintptr) {
	if dropMode.Load() != 0 {
		return
	}
	// The subclass callback runs on the window's own thread; the OLE target is
	// registered from there as well so COM calls are delivered via Gio's
	// message pump. SetWindowLongPtr itself only edits window data and is
	// safe to call from this (the main) thread.
	subclassCB = syscall.NewCallback(dropSubclassProc)
	slog.Info("native file drop: callback created")
	index := int32(gwlpWndProc) // sign-extends to the correct 64-bit -4
	ret, _, callErr := procSetWindowLongPtrW.Call(hwnd, uintptr(index), subclassCB)
	// LazyProc.Call returns syscall.Errno(0) as a non-nil error interface on
	// success. Checking err != nil here reports "The operation completed
	// successfully" as a failure, which prevented the subclass (and therefore
	// all drag & drop) from ever being installed.
	if errno, ok := callErr.(syscall.Errno); ok && errno == 0 {
		callErr = nil
	}
	slog.Info("native file drop: subclassed", "previous", ret, "err", callErr)
	if ret == 0 || callErr != nil {
		// No previous WndProc to chain to: never install a callback that
		// cannot forward messages, as that would break the window.
		slog.Info("native file drop: subclass unavailable, drag & drop disabled", "previous", ret, "err", callErr)
		return
	}
	oldWndProc.Store(ret)
	// Registering an OLE IDropTarget requires COM on the window thread; that
	// has been observed to stall startup on some systems. The classic shell
	// WM_DROPFILES path needs no COM and still gives real Explorer drops, so
	// it is the default. OLE (which adds the drag-over overlay) is opt-in.
	if os.Getenv("TRAYSCALE_OLE_DND") == "1" {
		procPostMessageW.Call(hwnd, wmInstallDrop, 0, 0)
	} else {
		procDragAcceptFiles.Call(hwnd, 1)
		dropMode.Store(2)
		slog.Info("native file drop: WM_DROPFILES mode (set TRAYSCALE_OLE_DND=1 for drag-over feedback)")
	}
}

func dropSubclassProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	// A panic cannot unwind through the Win32->Go callback boundary.
	defer func() { _ = recover() }()

	switch msg {
	case wmInstallDrop:
		installOLE(hwnd)
		return 0
	case wmDropFiles:
		handleWMDropFiles(wParam)
		return 0
	case wmDestroy, wmNCDestroy:
		cleanupOLE(hwnd)
	}
	if prev := oldWndProc.Load(); prev != 0 {
		r, _, _ := procCallWindowProcW.Call(prev, hwnd, uintptr(msg), wParam, lParam)
		return r
	}
	return 0
}

func installOLE(hwnd uintptr) {
	slog.Info("native file drop: initialising OLE on window thread")
	hr, _, _ := procOleInitialize.Call(0)
	slog.Info("native file drop: OleInitialize done", "hr", hr)
	if hr == sOK || hr == sFalse {
		oleInited = hr == sOK
	} else {
		// COM already initialised in an incompatible mode. OLE drag & drop
		// needs an STA, so use the shell fallback instead.
		oleInited = false
	}

	dt := &dropTarget{}
	dt.vtbl.queryInterface = syscall.NewCallback(diQueryInterface)
	dt.vtbl.addRef = syscall.NewCallback(diAddRef)
	dt.vtbl.release = syscall.NewCallback(diRelease)
	dt.vtbl.dragEnter = syscall.NewCallback(diDragEnter)
	dt.vtbl.dragOver = syscall.NewCallback(diDragOver)
	dt.vtbl.dragLeave = syscall.NewCallback(diDragLeave)
	dt.vtbl.drop = syscall.NewCallback(diDrop)
	dt.vtblPtr = &dt.vtbl
	dt.refs = 1

	slog.Info("native file drop: registering IDropTarget")
	hr, _, _ = procRegisterDragDrop.Call(hwnd, uintptr(unsafe.Pointer(&dt.vtblPtr)))
	slog.Info("native file drop: RegisterDragDrop done", "hr", hr)
	if hr == sOK {
		dropTargetObj = dt // keep alive for the lifetime of the window
		dropMode.Store(1)
	} else {
		dropMode.Store(2)
		if oleInited {
			procOleUninitialize.Call()
			oleInited = false
		}
	}
	// Also accept the legacy message as a belt-and-braces fallback.
	procDragAcceptFiles.Call(hwnd, 1)
	slog.Info("native file drop installed", "mode", NativeDropMode())
}

func cleanupOLE(hwnd uintptr) {
	if dropMode.Load() == 1 {
		procRevokeDragDrop.Call(hwnd)
	}
	dropMode.Store(0)
	if oleInited {
		procOleUninitialize.Call()
		oleInited = false
	}
}

// --- IDropTarget implementation -------------------------------------------

func diQueryInterface(this, riid, ppv uintptr) (ret uintptr) {
	defer func() {
		_ = recover()
		if ret == 0 {
			ret = eFail
		}
	}()
	if riid == 0 || ppv == 0 {
		return ePointer
	}
	g := (*guid)(ptr(riid))
	if *g == iidIDropTarget || *g == iidIUnknown {
		*(*uintptr)(ptr(ppv)) = this
		diAddRef(this)
		return sOK
	}
	*(*uintptr)(ptr(ppv)) = 0
	return eNoInterface
}

func diAddRef(this uintptr) (ret uintptr) {
	defer func() { _ = recover() }()
	if dt := (*dropTarget)(ptr(this)); dt != nil {
		return uintptr(atomic.AddUint32(&dt.refs, 1))
	}
	return 0
}

func diRelease(this uintptr) (ret uintptr) {
	defer func() { _ = recover() }()
	if dt := (*dropTarget)(ptr(this)); dt != nil {
		return uintptr(atomic.AddUint32(&dt.refs, ^uint32(0)))
	}
	return 0
}

// grfKeyState and pt are packed into single uintptr registers on amd64.
func diDragEnter(this, dataObj, keyState, pt, effect uintptr) (ret uintptr) {
	defer func() { _ = recover() }()
	setDropEffect(effect)
	dragOver.Store(true)
	if dataObj != 0 {
		if files, err := filesFromDataObject(dataObj); err == nil && len(files) > 0 {
			// Remember the candidate list so Drop can still use it if the
			// IDataObject becomes unavailable at drop time.
			candidateFiles = files
		}
	}
	invalidateUI()
	return sOK
}

func diDragOver(this, keyState, pt, effect uintptr) (ret uintptr) {
	defer func() { _ = recover() }()
	setDropEffect(effect)
	dragOver.Store(true)
	invalidateUI()
	return sOK
}

func diDragLeave(this uintptr) (ret uintptr) {
	defer func() { _ = recover() }()
	dragOver.Store(false)
	candidateFiles = nil
	invalidateUI()
	return sOK
}

func diDrop(this, dataObj, keyState, pt, effect uintptr) (ret uintptr) {
	defer func() { _ = recover() }()
	setDropEffect(effect)
	dragOver.Store(false)
	files := candidateFiles
	candidateFiles = nil
	if dataObj != 0 {
		if f, err := filesFromDataObject(dataObj); err == nil && len(f) > 0 {
			files = f
		}
	}
	pushDroppedFiles(files)
	invalidateUI()
	return sOK
}

var candidateFiles []string

func setDropEffect(effect uintptr) {
	if effect != 0 {
		*(*uint32)(ptr(effect)) = dropeffectCopy
	}
}

func filesFromDataObject(dataObj uintptr) ([]string, error) {
	vtbl := *(*uintptr)(ptr(dataObj))
	if vtbl == 0 {
		return nil, errors.New("null IDataObject vtable")
	}
	// IDataObject::GetData is the 4th method (3 IUnknown methods precede it).
	getData := *(*uintptr)(ptr(vtbl + 3*unsafe.Sizeof(uintptr(0))))
	fe := formatEtc{cfFormat: cfHDrop, dwAspect: dvaspectContent, tymed: tymedHGlobal}
	var med stgMedium
	r, _, _ := syscall.Syscall(getData, 3, dataObj, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&med)))
	if r != sOK || med.tymed != tymedHGlobal || med.hGlobal == 0 {
		return nil, errors.New("IDataObject::GetData(CF_HDROP) failed")
	}
	defer procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&med)))

	p, _, _ := procGlobalLock.Call(med.hGlobal)
	if p == 0 {
		return nil, errors.New("GlobalLock failed")
	}
	defer procGlobalUnlock.Call(med.hGlobal)

	sz, _, _ := procGlobalSize.Call(med.hGlobal)
	df := (*dropFiles)(ptr(p))
	off := int(df.pFiles)
	if off <= 0 || uintptr(off) >= sz {
		return nil, errors.New("bad DROPFILES offset")
	}
	data := unsafe.Slice((*byte)(ptr(p+uintptr(off))), int(sz)-off)
	if df.fWide != 0 {
		return parseWideFileList(data), nil
	}
	return parseANSIFileList(data), nil
}

func parseWideFileList(data []byte) []string {
	n := len(data) / 2
	words := unsafe.Slice((*uint16)(unsafe.Pointer(&data[0])), n)
	var out []string
	start := 0
	for i := 0; i < n; i++ {
		if words[i] != 0 {
			continue
		}
		if i == start {
			break
		}
		out = append(out, syscall.UTF16ToString(words[start:i]))
		start = i + 1
	}
	return out
}

func parseANSIFileList(data []byte) []string {
	var out []string
	start := 0
	for i, b := range data {
		if b != 0 {
			continue
		}
		if i == start {
			break
		}
		out = append(out, string(data[start:i]))
		start = i + 1
	}
	return out
}

func handleWMDropFiles(hDrop uintptr) {
	cnt, _, _ := procDragQueryFileW.Call(hDrop, ^uintptr(0), 0, 0)
	files := make([]string, 0, int(cnt))
	for i := uintptr(0); i < cnt; i++ {
		n, _, _ := procDragQueryFileW.Call(hDrop, i, 0, 0)
		if n == 0 || n > 32768 {
			continue
		}
		buf := make([]uint16, n+1)
		procDragQueryFileW.Call(hDrop, i, uintptr(unsafe.Pointer(&buf[0])), n+1)
		files = append(files, syscall.UTF16ToString(buf))
	}
	procDragFinish.Call(hDrop)
	pushDroppedFiles(files)
}

func pushDroppedFiles(files []string) {
	if len(files) == 0 {
		return
	}
	sink, ok := dropSink.Load().(chan<- []string)
	if !ok || sink == nil {
		return
	}
	select {
	case sink <- files:
	default:
		// Channel full: drop the oldest? Sending a fresh selection is more
		// useful, so just discard this batch rather than block the shell.
	}
}
