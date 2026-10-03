//go:build windows

package tray

import (
	"bytes"
	"image"
	"image/color"
	_ "image/png"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")
	user32 = windows.NewLazySystemDLL("user32.dll")

	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procGetDC              = user32.NewProc("GetDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type iconInfo struct {
	Icon     int32
	XHotspot uint32
	YHotspot uint32
	Mask     windows.Handle
	Color    windows.Handle
}

func hiconFromPNG(raw []byte, size int) (windows.Handle, error) {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	if size < 16 {
		size = 32
	}
	return hiconFromImage(scale(src, size))
}

func hiconFromImage(img *image.NRGBA) (windows.Handle, error) {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	hdc, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, hdc)

	var bih bitmapInfoHeader
	bih.Size = uint32(unsafe.Sizeof(bih))
	bih.Width = int32(w)
	bih.Height = int32(-h)
	bih.Planes = 1
	bih.BitCount = 32

	var bits unsafe.Pointer
	colorBmp, _, callErr := procCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&bih)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if colorBmp == 0 || bits == nil {
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return 0, callErr
		}
		return 0, windows.ERROR_INVALID_HANDLE
	}
	pix := unsafe.Slice((*byte)(bits), w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x, y)
			i := (y*w + x) * 4
			pix[i] = c.B
			pix[i+1] = c.G
			pix[i+2] = c.R
			pix[i+3] = c.A
		}
	}
	mask, _, _ := procCreateBitmap.Call(uintptr(w), uintptr(h), 1, 1, 0)
	var ii iconInfo
	ii.Icon = 1
	ii.Mask = windows.Handle(mask)
	ii.Color = windows.Handle(colorBmp)
	hicon, _, callErr := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	procDeleteObject.Call(colorBmp)
	if mask != 0 {
		procDeleteObject.Call(mask)
	}
	if hicon == 0 {
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return 0, callErr
		}
		return 0, windows.ERROR_INVALID_HANDLE
	}
	return windows.Handle(hicon), nil
}

func scale(src image.Image, size int) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			x0 := b.Min.X + x*b.Dx()/size
			x1 := b.Min.X + (x+1)*b.Dx()/size
			y0 := b.Min.Y + y*b.Dy()/size
			y1 := b.Min.Y + (y+1)*b.Dy()/size
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			var r, g, bl, a, n uint32
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, ca := src.At(xx, yy).RGBA()
					r += cr
					g += cg
					bl += cb
					a += ca
					n++
				}
			}
			if n == 0 {
				continue
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8((r / n) >> 8),
				G: uint8((g / n) >> 8),
				B: uint8((bl / n) >> 8),
				A: uint8((a / n) >> 8),
			})
		}
	}
	return dst
}
