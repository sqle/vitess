// Copyright 2014, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package binlog

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/gitql/vitess/go/mysqlconn/replication"

	binlogdatapb "github.com/gitql/vitess/go/vt/proto/binlogdata"
	querypb "github.com/gitql/vitess/go/vt/proto/query"
)

func sendTestEvents(channel chan<- replication.BinlogEvent, events []replication.BinlogEvent) {
	for _, ev := range events {
		channel <- ev
	}
	close(channel)
}

func TestStreamerParseEventsXID(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()
	s.ServerID = 62344

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewMariaDBGTIDEvent(f, s, replication.MariadbGTID{Domain: 0, Sequence: 0xd}, false /* hasBegin */),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewXIDEvent(f, s),
	}

	events := make(chan replication.BinlogEvent)

	want := []binlogdatapb.BinlogTransaction{
		{
			Statements: []*binlogdatapb.BinlogTransaction_Statement{
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_SET, Sql: []byte("SET TIMESTAMP=1407805592")},
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_INSERT, Sql: []byte("insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */")},
			},
			EventToken: &querypb.EventToken{
				Timestamp: 1407805592,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 0x0d,
					},
				}),
			},
		},
	}
	var got []binlogdatapb.BinlogTransaction
	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		got = append(got, *trans)
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	_, err := bls.parseEvents(context.Background(), events)
	if err != ErrServerEOF {
		t.Errorf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("binlogConnStreamer.parseEvents(): got:\n%v\nwant:\n%v", got, want)
	}
}

func TestStreamerParseEventsCommit(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()
	s.ServerID = 62344

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewMariaDBGTIDEvent(f, s, replication.MariadbGTID{Domain: 0, Sequence: 0xd}, false /* hasBegin */),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "COMMIT"}),
	}

	events := make(chan replication.BinlogEvent)

	want := []binlogdatapb.BinlogTransaction{
		{
			Statements: []*binlogdatapb.BinlogTransaction_Statement{
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_SET, Sql: []byte("SET TIMESTAMP=1407805592")},
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_INSERT, Sql: []byte("insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */")},
			},
			EventToken: &querypb.EventToken{
				Timestamp: 1407805592,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 0x0d,
					},
				}),
			},
		},
	}
	var got []binlogdatapb.BinlogTransaction
	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		got = append(got, *trans)
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	_, err := bls.parseEvents(context.Background(), events)
	if err != ErrServerEOF {
		t.Errorf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("binlogConnStreamer.parseEvents(): got %v, want %v", got, want)
	}
}

func TestStreamerStop(t *testing.T) {
	events := make(chan replication.BinlogEvent)

	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	// Start parseEvents(), but don't send it anything, so it just waits.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := bls.parseEvents(ctx, events)
		done <- err
	}()

	// close the context, expect the parser to return
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("wrong context interruption returned value: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Errorf("timed out waiting for binlogConnStreamer.Stop()")
	}
}

func TestStreamerParseEventsClientEOF(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewXIDEvent(f, s),
	}
	want := ErrClientEOF

	events := make(chan replication.BinlogEvent)

	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		return io.EOF
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	_, err := bls.parseEvents(context.Background(), events)
	if err != want {
		t.Errorf("wrong error, got %#v, want %#v", err, want)
	}
}

func TestStreamerParseEventsServerEOF(t *testing.T) {
	want := ErrServerEOF

	events := make(chan replication.BinlogEvent)
	close(events)

	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	_, err := bls.parseEvents(context.Background(), events)
	if err != want {
		t.Errorf("wrong error, got %#v, want %#v", err, want)
	}
}

func TestStreamerParseEventsSendErrorXID(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewXIDEvent(f, s),
	}
	want := "send reply error: foobar"

	events := make(chan replication.BinlogEvent)

	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		return fmt.Errorf("foobar")
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)

	_, err := bls.parseEvents(context.Background(), events)
	if err == nil {
		t.Errorf("expected error, got none")
		return
	}
	if got := err.Error(); got != want {
		t.Errorf("wrong error, got %#v, want %#v", got, want)
	}
}

func TestStreamerParseEventsSendErrorCommit(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "COMMIT"}),
	}
	want := "send reply error: foobar"

	events := make(chan replication.BinlogEvent)

	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		return fmt.Errorf("foobar")
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	_, err := bls.parseEvents(context.Background(), events)
	if err == nil {
		t.Errorf("expected error, got none")
		return
	}
	if got := err.Error(); got != want {
		t.Errorf("wrong error, got %#v, want %#v", got, want)
	}
}

func TestStreamerParseEventsInvalid(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewInvalidEvent(),
		replication.NewXIDEvent(f, s),
	}
	want := "can't parse binlog event, invalid data:"

	events := make(chan replication.BinlogEvent)

	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	_, err := bls.parseEvents(context.Background(), events)
	if err == nil {
		t.Errorf("expected error, got none")
		return
	}
	if got := err.Error(); !strings.HasPrefix(got, want) {
		t.Errorf("wrong error, got %#v, want %#v", got, want)
	}
}

func TestStreamerParseEventsInvalidFormat(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewInvalidFormatDescriptionEvent(f, s),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewXIDEvent(f, s),
	}
	want := "can't parse FORMAT_DESCRIPTION_EVENT:"

	events := make(chan replication.BinlogEvent)

	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	_, err := bls.parseEvents(context.Background(), events)
	if err == nil {
		t.Errorf("expected error, got none")
		return
	}
	if got := err.Error(); !strings.HasPrefix(got, want) {
		t.Errorf("wrong error, got %#v, want %#v", got, want)
	}
}

func TestStreamerParseEventsNoFormat(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		//replication.NewFormatDescriptionEvent(f, s),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewXIDEvent(f, s),
	}
	want := "got a real event before FORMAT_DESCRIPTION_EVENT:"

	events := make(chan replication.BinlogEvent)

	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	_, err := bls.parseEvents(context.Background(), events)
	if err == nil {
		t.Errorf("expected error, got none")
		return
	}
	if got := err.Error(); !strings.HasPrefix(got, want) {
		t.Errorf("wrong error, got %#v, want %#v", got, want)
	}
}

func TestStreamerParseEventsInvalidQuery(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewInvalidQueryEvent(f, s),
		replication.NewXIDEvent(f, s),
	}
	want := "can't get query from binlog event:"

	events := make(chan replication.BinlogEvent)

	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	_, err := bls.parseEvents(context.Background(), events)
	if err == nil {
		t.Errorf("expected error, got none")
		return
	}
	if got := err.Error(); !strings.HasPrefix(got, want) {
		t.Errorf("wrong error, got %#v, want %#v", got, want)
	}
}

func TestStreamerParseEventsRollback(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()
	s.ServerID = 62344

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewMariaDBGTIDEvent(f, s, replication.MariadbGTID{Domain: 0, Sequence: 0xd}, false /* hasBegin */),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "ROLLBACK"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewXIDEvent(f, s),
	}

	events := make(chan replication.BinlogEvent)

	want := []binlogdatapb.BinlogTransaction{
		{
			Statements: nil,
			EventToken: &querypb.EventToken{
				Timestamp: 1407805592,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 0x0d,
					},
				}),
			},
		},
		{
			Statements: []*binlogdatapb.BinlogTransaction_Statement{
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_SET, Sql: []byte("SET TIMESTAMP=1407805592")},
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_INSERT, Sql: []byte("insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */")},
			},
			EventToken: &querypb.EventToken{
				Timestamp: 1407805592,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 0x0d,
					},
				}),
			},
		},
	}
	var got []binlogdatapb.BinlogTransaction
	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		got = append(got, *trans)
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	if _, err := bls.parseEvents(context.Background(), events); err != ErrServerEOF {
		t.Errorf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("binlogConnStreamer.parseEvents(): got %v, want %v", got, want)
	}
}

func TestStreamerParseEventsDMLWithoutBegin(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()
	s.ServerID = 62344

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewMariaDBGTIDEvent(f, s, replication.MariadbGTID{Domain: 0, Sequence: 0xd}, false /* hasBegin */),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewXIDEvent(f, s),
	}

	events := make(chan replication.BinlogEvent)

	want := []binlogdatapb.BinlogTransaction{
		{
			Statements: []*binlogdatapb.BinlogTransaction_Statement{
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_SET, Sql: []byte("SET TIMESTAMP=1407805592")},
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_INSERT, Sql: []byte("insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */")},
			},
			EventToken: &querypb.EventToken{
				Timestamp: 1407805592,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 0x0d,
					},
				}),
			},
		},
		{
			Statements: nil,
			EventToken: &querypb.EventToken{
				Timestamp: 1407805592,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 0x0d,
					},
				}),
			},
		},
	}
	var got []binlogdatapb.BinlogTransaction
	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		got = append(got, *trans)
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	if _, err := bls.parseEvents(context.Background(), events); err != ErrServerEOF {
		t.Errorf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("binlogConnStreamer.parseEvents(): got:\n%v\nwant:\n%v", got, want)
	}
}

func TestStreamerParseEventsBeginWithoutCommit(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()
	s.ServerID = 62344

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewMariaDBGTIDEvent(f, s, replication.MariadbGTID{Domain: 0, Sequence: 0xd}, false /* hasBegin */),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewXIDEvent(f, s),
	}

	events := make(chan replication.BinlogEvent)

	want := []binlogdatapb.BinlogTransaction{
		{
			Statements: []*binlogdatapb.BinlogTransaction_Statement{
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_SET, Sql: []byte("SET TIMESTAMP=1407805592")},
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_INSERT, Sql: []byte("insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */")},
			},
			EventToken: &querypb.EventToken{
				Timestamp: 1407805592,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 0x0d,
					},
				}),
			},
		},
		{
			Statements: []*binlogdatapb.BinlogTransaction_Statement{},
			EventToken: &querypb.EventToken{
				Timestamp: 1407805592,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 0x0d,
					},
				}),
			},
		},
	}
	var got []binlogdatapb.BinlogTransaction
	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		got = append(got, *trans)
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	if _, err := bls.parseEvents(context.Background(), events); err != ErrServerEOF {
		t.Errorf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("binlogConnStreamer.parseEvents(): got:\n%v\nwant:\n%v", got, want)
	}
}

func TestStreamerParseEventsSetInsertID(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()
	s.ServerID = 62344

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewMariaDBGTIDEvent(f, s, replication.MariadbGTID{Domain: 0, Sequence: 0xd}, false /* hasBegin */),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewIntVarEvent(f, s, replication.IntVarInsertID, 101),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewXIDEvent(f, s),
	}

	events := make(chan replication.BinlogEvent)

	want := []binlogdatapb.BinlogTransaction{
		{
			Statements: []*binlogdatapb.BinlogTransaction_Statement{
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_SET, Sql: []byte("SET INSERT_ID=101")},
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_SET, Sql: []byte("SET TIMESTAMP=1407805592")},
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_INSERT, Sql: []byte("insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */")},
			},
			EventToken: &querypb.EventToken{
				Timestamp: 1407805592,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 0x0d,
					},
				}),
			},
		},
	}
	var got []binlogdatapb.BinlogTransaction
	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		got = append(got, *trans)
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	if _, err := bls.parseEvents(context.Background(), events); err != ErrServerEOF {
		t.Errorf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("binlogConnStreamer.parseEvents(): got %v, want %v", got, want)
	}
}

func TestStreamerParseEventsInvalidIntVar(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewIntVarEvent(f, s, replication.IntVarInvalidInt, 0), // Invalid intvar.
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewXIDEvent(f, s),
	}
	want := "can't parse INTVAR_EVENT:"

	events := make(chan replication.BinlogEvent)

	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	_, err := bls.parseEvents(context.Background(), events)
	if err == nil {
		t.Errorf("expected error, got none")
		return
	}
	if got := err.Error(); !strings.HasPrefix(got, want) {
		t.Errorf("wrong error, got %#v, want %#v", got, want)
	}
}

func TestStreamerParseEventsOtherDB(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()
	s.ServerID = 62344

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewMariaDBGTIDEvent(f, s, replication.MariadbGTID{Domain: 0, Sequence: 0xd}, false /* hasBegin */),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "other",
			SQL:      "INSERT INTO test values (3, 4)"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewXIDEvent(f, s),
	}

	events := make(chan replication.BinlogEvent)

	want := []binlogdatapb.BinlogTransaction{
		{
			Statements: []*binlogdatapb.BinlogTransaction_Statement{
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_SET, Sql: []byte("SET TIMESTAMP=1407805592")},
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_INSERT, Sql: []byte("insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */")},
			},
			EventToken: &querypb.EventToken{
				Timestamp: 1407805592,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 0x0d,
					},
				}),
			},
		},
	}
	var got []binlogdatapb.BinlogTransaction
	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		got = append(got, *trans)
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	if _, err := bls.parseEvents(context.Background(), events); err != ErrServerEOF {
		t.Errorf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("binlogConnStreamer.parseEvents(): got %v, want %v", got, want)
	}
}

func TestStreamerParseEventsOtherDBBegin(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()
	s.ServerID = 62344

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewMariaDBGTIDEvent(f, s, replication.MariadbGTID{Domain: 0, Sequence: 0xd}, false /* hasBegin */),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "other",
			SQL:      "BEGIN"}), // Check that this doesn't get filtered out.
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "other",
			SQL:      "INSERT INTO test values (3, 4)"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewXIDEvent(f, s),
	}

	events := make(chan replication.BinlogEvent)

	want := []binlogdatapb.BinlogTransaction{
		{
			Statements: []*binlogdatapb.BinlogTransaction_Statement{
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_SET, Sql: []byte("SET TIMESTAMP=1407805592")},
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_INSERT, Sql: []byte("insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */")},
			},
			EventToken: &querypb.EventToken{
				Timestamp: 1407805592,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 0x0d,
					},
				}),
			},
		},
	}
	var got []binlogdatapb.BinlogTransaction
	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		got = append(got, *trans)
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	if _, err := bls.parseEvents(context.Background(), events); err != ErrServerEOF {
		t.Errorf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("binlogConnStreamer.parseEvents(): got %v, want %v", got, want)
	}
}

func TestStreamerParseEventsBeginAgain(t *testing.T) {
	f := replication.NewMySQL56BinlogFormat()
	s := replication.NewFakeBinlogStream()

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 0, ""),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "insert into vt_a(eid, id) values (1, 1) /* _stream vt_a (eid id ) (1 1 ); */"}),
		replication.NewQueryEvent(f, s, replication.Query{
			Database: "vt_test_keyspace",
			SQL:      "BEGIN"}),
	}

	events := make(chan replication.BinlogEvent)

	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)
	before := binlogStreamerErrors.Counts()["ParseEvents"]

	go sendTestEvents(events, input)
	if _, err := bls.parseEvents(context.Background(), events); err != ErrServerEOF {
		t.Errorf("unexpected error: %v", err)
	}
	after := binlogStreamerErrors.Counts()["ParseEvents"]
	if got := after - before; got != 1 {
		t.Errorf("error count change = %v, want 1", got)
	}
}

// TestStreamerParseEventsMariadbStandaloneGTID tests a MariaDB server
// with no checksum, using a GTID with a Begin.
func TestStreamerParseEventsMariadbBeginGTID(t *testing.T) {
	f := replication.NewMariaDBBinlogFormat()
	s := replication.NewFakeBinlogStream()
	s.ServerID = 62344
	s.Timestamp = 1409892744

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 4, "filename.0001"),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewMariaDBGTIDEvent(f, s, replication.MariadbGTID{Domain: 0, Sequence: 10}, true /* hasBegin */),
		replication.NewQueryEvent(f, s, replication.Query{
			Charset: &binlogdatapb.Charset{Client: 33, Conn: 33, Server: 33},
			SQL:     "insert into vt_insert_test(msg) values ('test 0') /* _stream vt_insert_test (id ) (null ); */",
		}),
		replication.NewXIDEvent(f, s),
	}

	events := make(chan replication.BinlogEvent)

	want := []binlogdatapb.BinlogTransaction{
		{
			Statements: []*binlogdatapb.BinlogTransaction_Statement{
				{
					Category: binlogdatapb.BinlogTransaction_Statement_BL_SET,
					Charset:  &binlogdatapb.Charset{Client: 33, Conn: 33, Server: 33},
					Sql:      []byte("SET TIMESTAMP=1409892744"),
				},
				{
					Category: binlogdatapb.BinlogTransaction_Statement_BL_INSERT,
					Charset:  &binlogdatapb.Charset{Client: 33, Conn: 33, Server: 33},
					Sql:      []byte("insert into vt_insert_test(msg) values ('test 0') /* _stream vt_insert_test (id ) (null ); */"),
				},
			},
			EventToken: &querypb.EventToken{
				Timestamp: 1409892744,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 10,
					},
				}),
			},
		},
	}
	var got []binlogdatapb.BinlogTransaction
	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		got = append(got, *trans)
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	if _, err := bls.parseEvents(context.Background(), events); err != ErrServerEOF {
		t.Errorf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("binlogConnStreamer.parseEvents(): got:\n%v\nwant:\n%v", got, want)
	}
}

// TestStreamerParseEventsMariadbStandaloneGTID tests a MariaDB server
// with no checksum, using a standalone GTID.
func TestStreamerParseEventsMariadbStandaloneGTID(t *testing.T) {
	f := replication.NewMariaDBBinlogFormat()
	s := replication.NewFakeBinlogStream()
	s.ServerID = 62344
	s.Timestamp = 1409892744

	input := []replication.BinlogEvent{
		replication.NewRotateEvent(f, s, 4, "filename.0001"),
		replication.NewFormatDescriptionEvent(f, s),
		replication.NewMariaDBGTIDEvent(f, s, replication.MariadbGTID{Domain: 0, Sequence: 9}, false /* hasBegin */),
		replication.NewQueryEvent(f, s, replication.Query{
			Charset: &binlogdatapb.Charset{Client: 8, Conn: 8, Server: 33},
			SQL:     "create table if not exists vt_insert_test (\nid bigint auto_increment,\nmsg varchar(64),\nprimary key (id)\n) Engine=InnoDB",
		}),
	}

	events := make(chan replication.BinlogEvent)

	want := []binlogdatapb.BinlogTransaction{
		{
			Statements: []*binlogdatapb.BinlogTransaction_Statement{
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_SET, Charset: &binlogdatapb.Charset{Client: 8, Conn: 8, Server: 33}, Sql: []byte("SET TIMESTAMP=1409892744")},
				{Category: binlogdatapb.BinlogTransaction_Statement_BL_DDL, Charset: &binlogdatapb.Charset{Client: 8, Conn: 8, Server: 33}, Sql: []byte("create table if not exists vt_insert_test (\nid bigint auto_increment,\nmsg varchar(64),\nprimary key (id)\n) Engine=InnoDB")},
			},
			EventToken: &querypb.EventToken{
				Timestamp: 1409892744,
				Position: replication.EncodePosition(replication.Position{
					GTIDSet: replication.MariadbGTID{
						Domain:   0,
						Server:   62344,
						Sequence: 9,
					},
				}),
			},
		},
	}
	var got []binlogdatapb.BinlogTransaction
	sendTransaction := func(trans *binlogdatapb.BinlogTransaction) error {
		got = append(got, *trans)
		return nil
	}
	bls := NewStreamer("vt_test_keyspace", nil, nil, replication.Position{}, 0, sendTransaction)

	go sendTestEvents(events, input)
	if _, err := bls.parseEvents(context.Background(), events); err != ErrServerEOF {
		t.Errorf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("binlogConnStreamer.parseEvents(): got:\n%v\nwant:\n%v", got, want)
	}
}

func TestGetStatementCategory(t *testing.T) {
	table := map[string]binlogdatapb.BinlogTransaction_Statement_Category{
		"":  binlogdatapb.BinlogTransaction_Statement_BL_UNRECOGNIZED,
		" ": binlogdatapb.BinlogTransaction_Statement_BL_UNRECOGNIZED,
		" UPDATE we don't try to fix leading spaces": binlogdatapb.BinlogTransaction_Statement_BL_UNRECOGNIZED,
		"FOOBAR unknown query prefix":                binlogdatapb.BinlogTransaction_Statement_BL_UNRECOGNIZED,

		"BEGIN":    binlogdatapb.BinlogTransaction_Statement_BL_BEGIN,
		"COMMIT":   binlogdatapb.BinlogTransaction_Statement_BL_COMMIT,
		"ROLLBACK": binlogdatapb.BinlogTransaction_Statement_BL_ROLLBACK,
		"INSERT something (something, something)": binlogdatapb.BinlogTransaction_Statement_BL_INSERT,
		"UPDATE something SET something=nothing":  binlogdatapb.BinlogTransaction_Statement_BL_UPDATE,
		"DELETE something":                        binlogdatapb.BinlogTransaction_Statement_BL_DELETE,
		"CREATE something":                        binlogdatapb.BinlogTransaction_Statement_BL_DDL,
		"ALTER something":                         binlogdatapb.BinlogTransaction_Statement_BL_DDL,
		"DROP something":                          binlogdatapb.BinlogTransaction_Statement_BL_DDL,
		"TRUNCATE something":                      binlogdatapb.BinlogTransaction_Statement_BL_DDL,
		"RENAME something":                        binlogdatapb.BinlogTransaction_Statement_BL_DDL,
		"SET something=nothing":                   binlogdatapb.BinlogTransaction_Statement_BL_SET,
	}

	for input, want := range table {
		if got := getStatementCategory(input); got != want {
			t.Errorf("getStatementCategory(%v) = %v, want %v", input, got, want)
		}
	}
}
