//go:build ignore

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run genicon.go <output.ico>")
		os.Exit(2)
	}

	sizes := []int{16, 32, 48, 256}
	images := make([][]byte, 0, len(sizes))
	for _, size := range sizes {
		var data bytes.Buffer
		if err := png.Encode(&data, drawIcon(size)); err != nil {
			panic(err)
		}
		images = append(images, data.Bytes())
	}

	var output bytes.Buffer
	write(&output, uint16(0))
	write(&output, uint16(1))
	write(&output, uint16(len(images)))
	offset := 6 + 16*len(images)
	for i, data := range images {
		sizeByte := byte(sizes[i])
		if sizes[i] == 256 {
			sizeByte = 0
		}
		output.WriteByte(sizeByte)
		output.WriteByte(sizeByte)
		output.Write([]byte{0, 0})
		write(&output, uint16(1))
		write(&output, uint16(32))
		write(&output, uint32(len(data)))
		write(&output, uint32(offset))
		offset += len(data)
	}
	for _, data := range images {
		output.Write(data)
	}
	if err := os.WriteFile(os.Args[1], output.Bytes(), 0o644); err != nil {
		panic(err)
	}
}

func drawIcon(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	background := color.NRGBA{R: 14, G: 22, B: 40, A: 255}
	accent := color.NRGBA{R: 55, G: 128, B: 238, A: 255}
	white := color.NRGBA{R: 238, G: 246, B: 255, A: 255}
	green := color.NRGBA{R: 68, G: 211, B: 141, A: 255}

	margin := max(1, size/16)
	radius := max(2, size/5)
	fillRounded(img, margin, margin, size-margin, size-margin, radius, accent)
	border := max(1, size/18)
	fillRounded(img, margin+border, margin+border, size-margin-border, size-margin-border, radius-border, background)

	left, right := size*5/24, size*19/24
	top, bottom := size*6/24, size*18/24
	fillRounded(img, left, top, right, bottom, max(1, size/16), color.NRGBA{R: 24, G: 35, B: 58, A: 255})
	barHeight := max(1, size/10)
	fillRect(img, left, top, right, top+barHeight, accent)

	stroke := max(1, size/14)
	x1, x2 := size*8/24, size*11/24
	y1, y2, y3 := size*10/24, size*13/24, size*16/24
	drawLine(img, x1, y1, x2, y2, stroke, white)
	drawLine(img, x2, y2, x1, y3, stroke, white)
	fillRect(img, size*12/24, size*15/24, size*17/24, size*15/24+stroke, green)
	return img
}

func fillRounded(img *image.NRGBA, left, top, right, bottom, radius int, c color.NRGBA) {
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			dx := max(max(left+radius-x, 0), x-(right-radius-1))
			dy := max(max(top+radius-y, 0), y-(bottom-radius-1))
			if dx*dx+dy*dy <= radius*radius {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

func fillRect(img *image.NRGBA, left, top, right, bottom int, c color.NRGBA) {
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

func drawLine(img *image.NRGBA, x1, y1, x2, y2, width int, c color.NRGBA) {
	steps := max(abs(x2-x1), abs(y2-y1))
	for i := 0; i <= steps; i++ {
		x := x1 + (x2-x1)*i/max(1, steps)
		y := y1 + (y2-y1)*i/max(1, steps)
		fillRect(img, x-width/2, y-width/2, x+(width+1)/2, y+(width+1)/2, c)
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func write(output *bytes.Buffer, value any) {
	if err := binary.Write(output, binary.LittleEndian, value); err != nil {
		panic(err)
	}
}
