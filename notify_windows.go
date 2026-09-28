//go:build windows

package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The Windows toast, shown through the Windows Runtime's notification API
// over COM with nothing but golang.org/x/sys: activate an XmlDocument and load
// the toast into it, wrap it in a ToastNotification, and hand that to the
// notifier for taskr's AppUserModelID. The sequence is the one Microsoft
// documents for desktop apps; a COM object here is a pointer to a pointer to
// its method table, and a method is called by its index in that table (the
// first six belong to IUnknown and IInspectable).

var (
	combase                    = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize           = combase.NewProc("RoInitialize")
	procRoUninitialize         = combase.NewProc("RoUninitialize")
	procRoActivateInstance     = combase.NewProc("RoActivateInstance")
	procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString    = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = combase.NewProc("WindowsDeleteString")
)

var (
	iidXMLDocument                   = mustGUID("{F7F3A506-1E87-42D6-BCFB-B8C809FA5494}")
	iidXMLDocumentIO                 = mustGUID("{6CD0E74E-EE65-4489-9EBF-CA43E87BA637}")
	iidToastNotificationFactory      = mustGUID("{04124B20-82C6-4229-B109-FD9ED4662B53}")
	iidToastNotificationManagerStats = mustGUID("{50AC103F-D235-4598-BBEF-98FE4D1A3AD4}")
)

// Method-table slots past IUnknown (0–2) and IInspectable (3–5).
const (
	slotQueryInterface            = 0
	slotRelease                   = 2
	slotLoadXML                   = 6 // IXmlDocumentIO
	slotCreateToastNotification   = 6 // IToastNotificationFactory
	slotCreateToastNotifierWithID = 7 // IToastNotificationManagerStatics
	slotShow                      = 6 // IToastNotifier
)

// toastAppID is the AppUserModelID Windows files taskr's toasts under.
const toastAppID = "taskr"

const (
	roInitMultithreaded = 1
	rpcEChangedMode     = 0x80010106 // this thread already joined the single-threaded apartment
)

var registerToastApp = sync.OnceValue(func() error {
	// Windows shows toasts only for an app it knows by an AppUserModelID; an
	// unpackaged desktop app declares one under the current user. Per-user,
	// so no administrator is needed.
	key, _, err := registry.CreateKey(registry.CURRENT_USER,
		`Software\Classes\AppUserModelId\`+toastAppID, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue("DisplayName", "taskr")
})

func windowsToast(title, body string) error {
	if err := registerToastApp(); err != nil {
		return fmt.Errorf("registering taskr for notifications: %w", err)
	}
	return withWinRT(func() error {
		toast, err := newToastNotification(toastXML(title, body))
		if err != nil {
			return err
		}
		defer comRelease(toast)
		notifier, err := newToastNotifier(toastAppID)
		if err != nil {
			return err
		}
		defer comRelease(notifier)
		return comCall("Show", notifier, slotShow, uintptr(toast))
	})
}

// withWinRT runs fn with the Windows Runtime initialised on this goroutine's
// thread. Apartment membership belongs to an OS thread, so the goroutine stays
// on it for as long as fn holds COM objects.
func withWinRT(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch hr, _, _ := procRoInitialize.Call(roInitMultithreaded); {
	case int32(hr) >= 0: // S_OK, or S_FALSE when already initialised
		defer func() { _, _, _ = procRoUninitialize.Call() }()
	case uint32(hr) != rpcEChangedMode:
		return hresultError("RoInitialize", hr)
	}
	return fn()
}

// newToastNotification loads content into an XmlDocument and wraps it in a
// ToastNotification, which the caller releases.
func newToastNotification(content string) (unsafe.Pointer, error) {
	doc, err := activateInstance("Windows.Data.Xml.Dom.XmlDocument")
	if err != nil {
		return nil, err
	}
	defer comRelease(doc)
	docIO, err := queryInterface(doc, &iidXMLDocumentIO)
	if err != nil {
		return nil, err
	}
	defer comRelease(docIO)
	xmlText, err := newHString(content)
	if err != nil {
		return nil, err
	}
	defer deleteHString(xmlText)
	if err := comCall("LoadXml", docIO, slotLoadXML, xmlText); err != nil {
		return nil, err
	}
	xmlDoc, err := queryInterface(doc, &iidXMLDocument)
	if err != nil {
		return nil, err
	}
	defer comRelease(xmlDoc)

	factory, err := activationFactory("Windows.UI.Notifications.ToastNotification", &iidToastNotificationFactory)
	if err != nil {
		return nil, err
	}
	defer comRelease(factory)
	var toast unsafe.Pointer
	if err := comCall("CreateToastNotification", factory, slotCreateToastNotification, uintptr(xmlDoc), uintptr(unsafe.Pointer(&toast))); err != nil {
		return nil, err
	}
	return toast, nil
}

// newToastNotifier is the notifier for an AppUserModelID, which the caller
// releases.
func newToastNotifier(appID string) (unsafe.Pointer, error) {
	manager, err := activationFactory("Windows.UI.Notifications.ToastNotificationManager", &iidToastNotificationManagerStats)
	if err != nil {
		return nil, err
	}
	defer comRelease(manager)
	id, err := newHString(appID)
	if err != nil {
		return nil, err
	}
	defer deleteHString(id)
	var notifier unsafe.Pointer
	if err := comCall("CreateToastNotifierWithId", manager, slotCreateToastNotifierWithID, id, uintptr(unsafe.Pointer(&notifier))); err != nil {
		return nil, err
	}
	return notifier, nil
}

func activateInstance(class string) (unsafe.Pointer, error) {
	name, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer deleteHString(name)
	var obj unsafe.Pointer
	if hr, _, _ := procRoActivateInstance.Call(name, uintptr(unsafe.Pointer(&obj))); int32(hr) < 0 {
		return nil, hresultError("activating "+class, hr)
	}
	return obj, nil
}

func activationFactory(class string, iid *windows.GUID) (unsafe.Pointer, error) {
	name, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer deleteHString(name)
	var obj unsafe.Pointer
	if hr, _, _ := procRoGetActivationFactory.Call(name, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&obj))); int32(hr) < 0 {
		return nil, hresultError("getting the factory for "+class, hr)
	}
	return obj, nil
}

func queryInterface(obj unsafe.Pointer, iid *windows.GUID) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	if err := comCall("QueryInterface", obj, slotQueryInterface, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))); err != nil {
		return nil, err
	}
	return out, nil
}

// comCall calls method slot of obj's method table with obj as its receiver.
// Out-parameters arrive as pointers converted to uintptr, so the directive
// keeps what they point at on the heap and alive through the call: a stack
// the goroutine outgrew would otherwise move them from under the address.
//
//go:uintptrescapes
func comCall(name string, obj unsafe.Pointer, slot int, args ...uintptr) error {
	table := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(table, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	if int32(hr) < 0 {
		return hresultError(name, hr)
	}
	return nil
}

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		table := *(*unsafe.Pointer)(obj)
		fn := *(*uintptr)(unsafe.Add(table, uintptr(slotRelease)*unsafe.Sizeof(uintptr(0))))
		_, _, _ = syscall.SyscallN(fn, uintptr(obj))
	}
}

// newHString makes the Windows Runtime's string type from s; its caller
// deletes it.
func newHString(s string) (uintptr, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h uintptr
	if hr, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h))); int32(hr) < 0 {
		return 0, hresultError("WindowsCreateString", hr)
	}
	return h, nil
}

func deleteHString(h uintptr) {
	if h != 0 {
		_, _, _ = procWindowsDeleteString.Call(h)
	}
}

func hresultError(what string, hr uintptr) error {
	return fmt.Errorf("%s: HRESULT 0x%08X", what, uint32(hr))
}

func mustGUID(s string) windows.GUID {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic(err)
	}
	return g
}
