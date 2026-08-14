// Package pack implements the compact on-disk format for the static game
// database (nations, leagues, clubs, players).
//
// The format is a gzipped stream of fixed-width little-endian records plus a
// single deduplicated string table. Player records are the bulk of the file and
// cost 34 attribute bytes plus ~30 bytes of scalars each, so the whole of
// Europe's top divisions fits in a few hundred kilobytes and decodes with no
// per-record allocation beyond the string table.
package pack

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"sportsim/engine/model"
)

var magic = [4]byte{'S', 'P', 'S', 'M'}

const version uint16 = 1

// Data is the decoded static database.
type Data struct {
	Nations []model.Nation
	Leagues []model.League
	Clubs   []model.Club
	Players []model.Player
}

// ---------- writer ----------

type writer struct {
	buf     bytes.Buffer
	strings []string
	index   map[string]uint32
}

func newWriter() *writer {
	w := &writer{index: make(map[string]uint32)}
	w.intern("") // ID 0 is always the empty string
	return w
}

func (w *writer) intern(s string) uint32 {
	if id, ok := w.index[s]; ok {
		return id
	}
	id := uint32(len(w.strings))
	w.strings = append(w.strings, s)
	w.index[s] = id
	return id
}

func (w *writer) u8(v uint8)   { w.buf.WriteByte(v) }
func (w *writer) u16(v uint16) { binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *writer) u32(v uint32) { binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *writer) i64(v int64)  { binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *writer) str(s string) { w.u32(w.intern(s)) }

// ---------- reader ----------

type reader struct {
	b       []byte
	off     int
	strings []string
	err     error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return make([]byte, n)
	}
	if r.off+n > len(r.b) {
		r.err = io.ErrUnexpectedEOF
		return make([]byte, n)
	}
	s := r.b[r.off : r.off+n]
	r.off += n
	return s
}

func (r *reader) u8() uint8   { return r.take(1)[0] }
func (r *reader) u16() uint16 { return binary.LittleEndian.Uint16(r.take(2)) }
func (r *reader) u32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }
func (r *reader) i64() int64  { return int64(binary.LittleEndian.Uint64(r.take(8))) }

func (r *reader) str() string {
	id := r.u32()
	if int(id) >= len(r.strings) {
		if r.err == nil {
			r.err = fmt.Errorf("pack: string id %d out of range", id)
		}
		return ""
	}
	return r.strings[id]
}

// ---------- encode ----------

// Encode serialises the database and writes the gzipped result to out.
func Encode(out io.Writer, d *Data) error {
	w := newWriter()

	// Bodies are written first so that every string is interned before the
	// table is emitted; the final stream puts the table ahead of the bodies.
	w.u32(uint32(len(d.Nations)))
	for i := range d.Nations {
		n := &d.Nations[i]
		w.u16(n.ID)
		w.str(n.Name)
		w.str(n.Code)
	}

	w.u32(uint32(len(d.Leagues)))
	for i := range d.Leagues {
		l := &d.Leagues[i]
		w.u16(l.ID)
		w.str(l.Name)
		w.u16(l.NationID)
		w.str(l.Country)
		w.u8(l.Tier)
		w.u8(l.Promoted)
		w.u8(l.Relegated)
		w.u8(l.Reputation)
		w.i64(l.PrizeMoney)
		w.u16(uint16(len(l.ClubIDs)))
		for _, c := range l.ClubIDs {
			w.u16(c)
		}
	}

	w.u32(uint32(len(d.Clubs)))
	for i := range d.Clubs {
		c := &d.Clubs[i]
		w.u16(c.ID)
		w.str(c.Name)
		w.str(c.Short)
		w.u16(c.LeagueID)
		w.u16(c.NationID)
		w.u8(c.Reputation)
		w.u32(c.StadiumCap)
		w.i64(c.Balance)
		w.i64(c.TransferBudget)
		w.i64(c.WageBudget)
		w.u8(c.TrainingFacilities)
		w.u8(c.YouthFacilities)
		w.u8(c.YouthRecruitment)
	}

	w.u32(uint32(len(d.Players)))
	for i := range d.Players {
		p := &d.Players[i]
		w.u32(p.ID)
		w.u16(p.ClubID)
		w.u16(p.NationID)
		w.str(p.Name)
		w.str(p.FullName)
		w.u16(p.BirthYear)
		w.u8(p.BirthMonth)
		w.u8(p.BirthDay)
		w.u8(p.HeightCM)
		w.u8(p.WeightKG)
		w.u8(p.NumPositions)
		for j := 0; j < 3; j++ {
			w.u8(uint8(p.Positions[j]))
		}
		w.buf.Write(p.Attr[:])
		w.u8(p.Potential)
		w.u8(p.Foot)
		w.u8(p.WeakFoot)
		w.u8(p.SkillMoves)
		w.u8(p.IntRep)
		w.u8(p.WorkRateAtk)
		w.u8(p.WorkRateDef)
		w.u32(p.ValueEUR)
		w.u32(p.WageEUR)
		w.u16(p.ContractUntil)
		w.u8(p.Jersey)
	}

	// Assemble: header, string table, bodies.
	var head bytes.Buffer
	head.Write(magic[:])
	binary.Write(&head, binary.LittleEndian, version)
	binary.Write(&head, binary.LittleEndian, uint32(len(w.strings)))
	for _, s := range w.strings {
		binary.Write(&head, binary.LittleEndian, uint16(len(s)))
		head.WriteString(s)
	}

	gz, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := gz.Write(head.Bytes()); err != nil {
		return err
	}
	if _, err := gz.Write(w.buf.Bytes()); err != nil {
		return err
	}
	return gz.Close()
}

// ---------- decode ----------

// Decode reads a gzipped pack stream.
func Decode(in io.Reader) (*Data, error) {
	gz, err := gzip.NewReader(in)
	if err != nil {
		return nil, fmt.Errorf("pack: %w", err)
	}
	defer gz.Close()
	raw, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("pack: %w", err)
	}

	r := &reader{b: raw}
	if got := r.take(4); !bytes.Equal(got, magic[:]) {
		return nil, errors.New("pack: bad magic, not a SportSim data file")
	}
	if v := r.u16(); v != version {
		return nil, fmt.Errorf("pack: unsupported version %d (want %d)", v, version)
	}
	nStr := r.u32()
	if r.err != nil {
		return nil, r.err
	}
	r.strings = make([]string, 0, nStr)
	for i := uint32(0); i < nStr; i++ {
		l := int(r.u16())
		r.strings = append(r.strings, string(r.take(l)))
		if r.err != nil {
			return nil, r.err
		}
	}

	d := &Data{}

	d.Nations = make([]model.Nation, r.u32())
	for i := range d.Nations {
		n := &d.Nations[i]
		n.ID = r.u16()
		n.Name = r.str()
		n.Code = r.str()
	}

	d.Leagues = make([]model.League, r.u32())
	for i := range d.Leagues {
		l := &d.Leagues[i]
		l.ID = r.u16()
		l.Name = r.str()
		l.NationID = r.u16()
		l.Country = r.str()
		l.Tier = r.u8()
		l.Promoted = r.u8()
		l.Relegated = r.u8()
		l.Reputation = r.u8()
		l.PrizeMoney = r.i64()
		l.ClubIDs = make([]uint16, r.u16())
		for j := range l.ClubIDs {
			l.ClubIDs[j] = r.u16()
		}
	}

	d.Clubs = make([]model.Club, r.u32())
	for i := range d.Clubs {
		c := &d.Clubs[i]
		c.ID = r.u16()
		c.Name = r.str()
		c.Short = r.str()
		c.LeagueID = r.u16()
		c.NationID = r.u16()
		c.Reputation = r.u8()
		c.StadiumCap = r.u32()
		c.Balance = r.i64()
		c.TransferBudget = r.i64()
		c.WageBudget = r.i64()
		c.TrainingFacilities = r.u8()
		c.YouthFacilities = r.u8()
		c.YouthRecruitment = r.u8()
		c.Tactics = model.DefaultTactics()
	}

	d.Players = make([]model.Player, r.u32())
	for i := range d.Players {
		p := &d.Players[i]
		p.ID = r.u32()
		p.ClubID = r.u16()
		p.NationID = r.u16()
		p.Name = r.str()
		p.FullName = r.str()
		p.BirthYear = r.u16()
		p.BirthMonth = r.u8()
		p.BirthDay = r.u8()
		p.HeightCM = r.u8()
		p.WeightKG = r.u8()
		p.NumPositions = r.u8()
		for j := 0; j < 3; j++ {
			p.Positions[j] = model.Pos(r.u8())
		}
		copy(p.Attr[:], r.take(model.NumAttr))
		p.Potential = r.u8()
		p.Foot = r.u8()
		p.WeakFoot = r.u8()
		p.SkillMoves = r.u8()
		p.IntRep = r.u8()
		p.WorkRateAtk = r.u8()
		p.WorkRateDef = r.u8()
		p.ValueEUR = r.u32()
		p.WageEUR = r.u32()
		p.ContractUntil = r.u16()
		p.Jersey = r.u8()
	}

	if r.err != nil {
		return nil, r.err
	}
	return d, nil
}
