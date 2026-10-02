package pipewire

import "encoding/binary"

// SPA pod types (spa/utils/type.h).
const (
	typeID        = 3
	typeInt       = 4
	typeLong      = 5
	typeRectangle = 10
	typeFraction  = 11
	typeObject    = 15
	typeChoice    = 19
)

// SPA object types and param ids (spa/utils/type.h, spa/param/param.h).
const (
	objectFormat       = 0x40003
	objectParamBuffers = 0x40004
	objectParamMeta    = 0x40005

	paramEnumFormat = 3
	paramFormat     = 4
	paramBuffers    = 5
	paramMeta       = 6
)

// Format keys and values (spa/param/format.h, video/raw.h).
const (
	formatMediaType     = 1
	formatMediaSubtype  = 2
	formatVideoFormat   = 0x20001
	formatVideoModifier = 0x20002
	formatVideoSize     = 0x20003
	formatVideoRate     = 0x20004

	mediaTypeVideo  = 2
	mediaSubtypeRaw = 1
)

// VideoFormat is an spa_video_format.
type VideoFormat uint32

// The packed 8-bit formats compositors hand out.
const (
	VideoRGBx VideoFormat = 7
	VideoBGRx VideoFormat = 8
	VideoRGBA VideoFormat = 11
	VideoBGRA VideoFormat = 12
	// The packed 24-bit formats GPU renderers offer over shm.
	VideoRGB VideoFormat = 15
	VideoBGR VideoFormat = 16
)

// Buffers and meta keys (spa/param/buffers.h).
const (
	buffersBuffers  = 1
	buffersBlocks   = 2
	buffersSize     = 3
	buffersStride   = 4
	buffersAlign    = 5
	buffersDataType = 6

	metaKeyType = 1
	metaKeySize = 2
)

// Data and meta types (spa/buffer/buffer.h, meta.h).
const (
	dataMemPtr = 1
	dataMemFd  = 2
	dataDmaBuf = 3

	// spa_data flags.
	dataFlagReadable = 1 << 0
	dataFlagMappable = 1 << 3

	metaHeader         = 1
	metaVideoDamage    = 3
	metaVideoTransform = 8

	metaHeaderSize    = 32 // spa_meta_header
	metaTransformSize = 4  // spa_meta_videotransform
	metaRegionSize    = 16 // spa_meta_region
)

const (
	choiceRange       = 1
	propFlagMandatory = 1 << 3
)

// pod is one serialized SPA pod value: its type and body.
type pod struct {
	typ  uint32
	body []byte
}

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func podID(v uint32) pod  { return pod{typeID, le32(v)} }
func podInt(v int32) pod  { return pod{typeInt, le32(uint32(v))} }
func podLong(v int64) pod { return pod{typeLong, binary.LittleEndian.AppendUint64(nil, uint64(v))} }

func podRectangle(w, h uint32) pod { return pod{typeRectangle, append(le32(w), le32(h)...)} }

func podFraction(num, denom uint32) pod { return pod{typeFraction, append(le32(num), le32(denom)...)} }

// podIntRange is a Choice of Range over Ints: default, min, max.
func podIntRange(def, lo, hi int32) pod {
	body := append(le32(choiceRange), le32(0)...)             // choice type, flags
	body = append(body, append(le32(4), le32(typeInt)...)...) // child pod: size, type
	for _, v := range []int32{def, lo, hi} {
		body = append(body, le32(uint32(v))...)
	}
	return pod{typeChoice, body}
}

// bytes is the pod with its header, padded to 8 bytes as pods are laid
// out.
func (p pod) bytes() []byte {
	out := append(le32(uint32(len(p.body))), le32(p.typ)...)
	out = append(out, p.body...)
	for len(out)%8 != 0 {
		out = append(out, 0)
	}
	return out
}

// prop is one object property.
type prop struct {
	key, flags uint32
	value      pod
}

// object serializes an spa_pod_object.
func object(typ, id uint32, props ...prop) []byte {
	body := append(le32(typ), le32(id)...)
	for _, p := range props {
		body = append(body, le32(p.key)...)
		body = append(body, le32(p.flags)...)
		body = append(body, p.value.bytes()...)
	}
	return pod{typeObject, body}.bytes()
}

// formatPod is the EnumFormat object for one offered video format; a
// modifier marks a dmabuf format (mandatory), none the SHM one.
func formatPod(width, height, fps uint32, format VideoFormat, modifier *uint64) []byte {
	props := []prop{
		{key: formatMediaType, value: podID(mediaTypeVideo)},
		{key: formatMediaSubtype, value: podID(mediaSubtypeRaw)},
		{key: formatVideoFormat, value: podID(uint32(format))},
	}
	if modifier != nil {
		props = append(props, prop{key: formatVideoModifier, flags: propFlagMandatory, value: podLong(int64(*modifier))})
	}
	props = append(props,
		prop{key: formatVideoSize, value: podRectangle(width, height)},
		prop{key: formatVideoRate, value: podFraction(fps, 1)},
	)
	return object(objectFormat, paramEnumFormat, props...)
}

// maxDamageRegions is how many damage rectangles a buffer's meta has
// room for (xdph's MAX_DAMAGE); more collapse into one full frame.
const maxDamageRegions = 4

// bufferPods is the buffer layout and the Header, VideoTransform and
// VideoDamage metas, declared once a format is negotiated: without the
// layout the server maps empty buffers and consumers show nothing.
func bufferPods(stride, height uint32) [][]byte {
	return bufferPodsOf(stride, height, 1<<dataMemFd|1<<dataMemPtr)
}

// bufferPodsOf is bufferPods with the buffers' data types (a bit mask of
// the spa_data types the producer provides).
func bufferPodsOf(stride, height, dataTypes uint32) [][]byte {
	return [][]byte{
		object(objectParamBuffers, paramBuffers,
			prop{key: buffersBuffers, value: podIntRange(4, 2, 16)},
			prop{key: buffersBlocks, value: podInt(1)},
			prop{key: buffersSize, value: podInt(int32(stride * height))},
			prop{key: buffersStride, value: podInt(int32(stride))},
			prop{key: buffersAlign, value: podInt(16)},
			prop{key: buffersDataType, value: podInt(int32(dataTypes))},
		),
		object(objectParamMeta, paramMeta,
			prop{key: metaKeyType, value: podID(metaHeader)},
			prop{key: metaKeySize, value: podInt(metaHeaderSize)},
		),
		object(objectParamMeta, paramMeta,
			prop{key: metaKeyType, value: podID(metaVideoTransform)},
			prop{key: metaKeySize, value: podInt(metaTransformSize)},
		),
		object(objectParamMeta, paramMeta,
			prop{key: metaKeyType, value: podID(metaVideoDamage)},
			prop{key: metaKeySize, value: podIntRange(metaRegionSize*maxDamageRegions, metaRegionSize, metaRegionSize*maxDamageRegions)},
		),
	}
}

// objectHas reports whether the spa_pod_object in b carries the
// property key: a negotiated format with the modifier is a dmabuf one.
func objectHas(b []byte, key uint32) bool {
	u32 := func(i int) uint32 { return binary.LittleEndian.Uint32(b[i:]) }
	if len(b) < 16 || u32(4) != typeObject {
		return false
	}
	end := min(len(b), 8+int(u32(0)))
	for i := 16; i+16 <= end; {
		if u32(i) == key {
			return true
		}
		size := int(u32(i + 8))
		i += 16 + (size+7)&^7
	}
	return false
}
