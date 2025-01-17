// Copyright 2015, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tabletserver

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/context"

	"github.com/gitql/vitess/go/mysqlconn"
	"github.com/gitql/vitess/go/mysqlconn/fakesqldb"
	"github.com/gitql/vitess/go/sqldb"
	"github.com/gitql/vitess/go/sqltypes"
	"github.com/gitql/vitess/go/vt/callerid"
	"github.com/gitql/vitess/go/vt/callinfo"
	"github.com/gitql/vitess/go/vt/callinfo/fakecallinfo"
	"github.com/gitql/vitess/go/vt/schema"
	"github.com/gitql/vitess/go/vt/tableacl"
	"github.com/gitql/vitess/go/vt/tableacl/simpleacl"
	"github.com/gitql/vitess/go/vt/tabletserver/planbuilder"
	"github.com/gitql/vitess/go/vt/tabletserver/tabletenv"

	querypb "github.com/gitql/vitess/go/vt/proto/query"
	tableaclpb "github.com/gitql/vitess/go/vt/proto/tableacl"
	topodatapb "github.com/gitql/vitess/go/vt/proto/topodata"
	vtrpcpb "github.com/gitql/vitess/go/vt/proto/vtrpc"
)

func TestQueryExecutorPlanDDL(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "alter table test_table add zipcode int"
	want := &sqltypes.Result{}
	db.AddQuery(query, want)
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanDDL, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
}

func TestQueryExecutorPlanPassDmlStrictMode(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "update test_table set pk = foo()"
	want := &sqltypes.Result{}
	db.AddQuery(query, want)
	ctx := context.Background()
	// non strict mode
	tsv := newTestTabletServer(ctx, noFlags, db)
	txid := newTransaction(tsv)
	qre := newTestQueryExecutor(ctx, tsv, query, txid)
	checkPlanID(t, planbuilder.PlanPassDML, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
	wantqueries := []string{query}
	gotqueries := fetchRecordedQueries(qre)
	if !reflect.DeepEqual(gotqueries, wantqueries) {
		t.Errorf("queries: %v, want %v", gotqueries, wantqueries)
	}
	testCommitHelper(t, tsv, qre)
	tsv.StopService()

	// strict mode
	tsv = newTestTabletServer(ctx, enableStrict, db)
	qre = newTestQueryExecutor(ctx, tsv, query, newTransaction(tsv))
	defer tsv.StopService()
	defer testCommitHelper(t, tsv, qre)
	checkPlanID(t, planbuilder.PlanPassDML, qre.plan.PlanID)
	got, err = qre.Execute()
	if err == nil {
		t.Fatal("qre.Execute() = nil, want error")
	}
	tabletError, ok := err.(*tabletenv.TabletError)
	if !ok {
		t.Fatalf("got: %v, want: a tabletenv.TabletError", tabletError)
	}
	if tabletError.ErrorCode != vtrpcpb.ErrorCode_BAD_INPUT {
		t.Fatalf("got: %s, want: BAD_INPUT", tabletError.ErrorCode)
	}
}

func TestQueryExecutorPlanPassDmlStrictModeAutoCommit(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "update test_table set pk = foo()"
	want := &sqltypes.Result{}
	db.AddQuery(query, want)
	// non strict mode
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, noFlags, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	checkPlanID(t, planbuilder.PlanPassDML, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
	tsv.StopService()

	// strict mode
	// update should fail because strict mode is not enabled
	tsv = newTestTabletServer(ctx, enableStrict, db)
	qre = newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanPassDML, qre.plan.PlanID)
	_, err = qre.Execute()
	if err == nil {
		t.Fatal("got: nil, want: error")
	}
	tabletError, ok := err.(*tabletenv.TabletError)
	if !ok {
		t.Fatalf("got: %v, want: *tabletenv.TabletError", tabletError)
	}
	if tabletError.ErrorCode != vtrpcpb.ErrorCode_BAD_INPUT {
		t.Fatalf("got: %s, want: BAD_INPUT", tabletError.ErrorCode)
	}
}

func TestQueryExecutorPlanInsertPk(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	db.AddQuery("insert into test_table values (1) /* _stream test_table (pk ) (1 ); */", &sqltypes.Result{})
	want := &sqltypes.Result{}
	query := "insert into test_table values(1)"
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanInsertPK, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
}

func TestQueryExecutorPlanInsertMessage(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	db.AddQueryPattern("insert into msg\\(time_scheduled, id, message, time_next, time_created, epoch\\) values \\(1, 2, 3, 1,.*", &sqltypes.Result{})
	db.AddQuery(
		"select time_next, epoch, id, message from msg where (time_scheduled = 1 and id = 2)",
		&sqltypes.Result{
			Fields: []*querypb.Field{
				{Type: sqltypes.Int64},
				{Type: sqltypes.Int64},
				{Type: sqltypes.Int64},
				{Type: sqltypes.Int64},
			},
			RowsAffected: 1,
			Rows: [][]sqltypes.Value{{
				sqltypes.MakeString([]byte("1")),
				sqltypes.MakeString([]byte("0")),
				sqltypes.MakeString([]byte("1")),
				sqltypes.MakeString([]byte("01")),
			}},
		},
	)
	want := &sqltypes.Result{}
	query := "insert into msg(time_scheduled, id, message) values(1, 2, 3)"
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanInsertMessage, qre.plan.PlanID)
	r1 := newTestReceiver(1)
	tsv.messager.schemaChanged(map[string]*TableInfo{
		"msg": {
			Table: &schema.Table{
				Type: schema.Message,
			},
		},
	})
	tsv.messager.Subscribe("msg", r1.rcv)
	<-r1.ch
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
	mr := <-r1.ch
	wantqr := &sqltypes.Result{
		Rows: [][]sqltypes.Value{{
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("1")),
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("01")),
		}},
	}
	if !reflect.DeepEqual(mr, wantqr) {
		t.Errorf("rows:\n%+v, want\n%+v", mr, wantqr)
	}

	txid := newTransaction(tsv)
	qre = newTestQueryExecutor(ctx, tsv, query, txid)
	defer testCommitHelper(t, tsv, qre)
	got, err = qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
}

func TestQueryExecutorPlanInsertSubQueryAutoCommmit(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "insert into test_table(pk) select pk from test_table where pk = 1 limit 1000"
	want := &sqltypes.Result{}
	db.AddQuery(query, want)
	selectQuery := "select pk from test_table where pk = 1 limit 1000"
	db.AddQuery(selectQuery, &sqltypes.Result{
		Fields: []*querypb.Field{{
			Name: "pk",
			Type: sqltypes.Int32,
		}},
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{
			{sqltypes.MakeTrusted(sqltypes.Int32, []byte("2"))},
		},
	})

	insertQuery := "insert into test_table(pk) values (2) /* _stream test_table (pk ) (2 ); */"

	db.AddQuery(insertQuery, &sqltypes.Result{})
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanInsertSubquery, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
}

func TestQueryExecutorPlanInsertSubQuery(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "insert into test_table(pk) select pk from test_table where pk = 1 limit 1000"
	want := &sqltypes.Result{}
	db.AddQuery(query, want)
	selectQuery := "select pk from test_table where pk = 1 limit 1000"
	db.AddQuery(selectQuery, &sqltypes.Result{
		Fields: []*querypb.Field{{
			Name: "pk",
			Type: sqltypes.Int32,
		}},
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{
			{sqltypes.MakeTrusted(sqltypes.Int32, []byte("2"))},
		},
	})

	insertQuery := "insert into test_table(pk) values (2) /* _stream test_table (pk ) (2 ); */"

	db.AddQuery(insertQuery, &sqltypes.Result{})
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	txid := newTransaction(tsv)
	qre := newTestQueryExecutor(ctx, tsv, query, txid)

	defer tsv.StopService()
	defer testCommitHelper(t, tsv, qre)
	checkPlanID(t, planbuilder.PlanInsertSubquery, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
	wantqueries := []string{"insert into test_table(pk) values (2) /* _stream test_table (pk ) (2 ); */"}
	gotqueries := fetchRecordedQueries(qre)
	if !reflect.DeepEqual(gotqueries, wantqueries) {
		t.Errorf("queries: %v, want %v", gotqueries, wantqueries)
	}
}

func TestQueryExecutorPlanUpsertPk(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	db.AddQuery("insert into test_table values (1) /* _stream test_table (pk ) (1 ); */", &sqltypes.Result{})
	want := &sqltypes.Result{}
	query := "insert into test_table values(1) on duplicate key update val=1"
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	txid := newTransaction(tsv)
	qre := newTestQueryExecutor(ctx, tsv, query, txid)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanUpsertPK, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
	wantqueries := []string{"insert into test_table values (1) /* _stream test_table (pk ) (1 ); */"}
	gotqueries := fetchRecordedQueries(qre)
	if !reflect.DeepEqual(gotqueries, wantqueries) {
		t.Errorf("queries: %v, want %v", gotqueries, wantqueries)
	}
	testCommitHelper(t, tsv, qre)

	db.AddRejectedQuery("insert into test_table values (1) /* _stream test_table (pk ) (1 ); */", errRejected)
	txid = newTransaction(tsv)
	qre = newTestQueryExecutor(ctx, tsv, query, txid)
	_, err = qre.Execute()
	wantErr := "error: rejected"
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Errorf("qre.Execute() = %v, want %v", err, wantErr)
	}
	if gotqueries = fetchRecordedQueries(qre); gotqueries != nil {
		t.Errorf("queries: %v, want nil", gotqueries)
	}
	testCommitHelper(t, tsv, qre)

	db.AddRejectedQuery(
		"insert into test_table values (1) /* _stream test_table (pk ) (1 ); */",
		sqldb.NewSQLError(mysqlconn.ERDupEntry, mysqlconn.SSDupKey, "err"),
	)
	db.AddQuery("update test_table set val = 1 where pk in (1) /* _stream test_table (pk ) (1 ); */", &sqltypes.Result{})
	txid = newTransaction(tsv)
	qre = newTestQueryExecutor(ctx, tsv, query, txid)
	_, err = qre.Execute()
	wantErr = "error: err (errno 1062) (sqlstate 23000)"
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Errorf("qre.Execute() = %v, want %v", err, wantErr)
	}
	wantqueries = []string{}
	if gotqueries = fetchRecordedQueries(qre); gotqueries != nil {
		t.Errorf("queries: %v, want nil", gotqueries)
	}
	testCommitHelper(t, tsv, qre)

	db.AddRejectedQuery(
		"insert into test_table values (1) /* _stream test_table (pk ) (1 ); */",
		sqldb.NewSQLError(mysqlconn.ERDupEntry, mysqlconn.SSDupKey, "ERROR 1062 (23000): Duplicate entry '2' for key 'PRIMARY'"),
	)
	db.AddQuery(
		"update test_table set val = 1 where pk in (1) /* _stream test_table (pk ) (1 ); */",
		&sqltypes.Result{RowsAffected: 1},
	)
	txid = newTransaction(tsv)
	qre = newTestQueryExecutor(ctx, tsv, query, txid)
	got, err = qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	want = &sqltypes.Result{
		RowsAffected: 2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got: %v, want: %v", got, want)
	}
	wantqueries = []string{"update test_table set val = 1 where pk in (1) /* _stream test_table (pk ) (1 ); */"}
	gotqueries = fetchRecordedQueries(qre)
	if !reflect.DeepEqual(gotqueries, wantqueries) {
		t.Errorf("queries: %v, want %v", gotqueries, wantqueries)
	}
	testCommitHelper(t, tsv, qre)
}

func TestQueryExecutorPlanUpsertPkAutoCommit(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	db.AddQuery("insert into test_table values (1) /* _stream test_table (pk ) (1 ); */", &sqltypes.Result{})
	want := &sqltypes.Result{}
	query := "insert into test_table values(1) on duplicate key update val=1"
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanUpsertPK, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}

	db.AddRejectedQuery("insert into test_table values (1) /* _stream test_table (pk ) (1 ); */", errRejected)
	_, err = qre.Execute()
	wantErr := "error: rejected"
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("qre.Execute() = %v, want %v", err, wantErr)
	}

	db.AddRejectedQuery(
		"insert into test_table values (1) /* _stream test_table (pk ) (1 ); */",
		sqldb.NewSQLError(mysqlconn.ERDupEntry, mysqlconn.SSDupKey, "err"),
	)
	db.AddQuery("update test_table set val = 1 where pk in (1) /* _stream test_table (pk ) (1 ); */", &sqltypes.Result{})
	_, err = qre.Execute()
	wantErr = "error: err (errno 1062) (sqlstate 23000)"
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("qre.Execute() = %v, want %v", err, wantErr)
	}

	db.AddRejectedQuery(
		"insert into test_table values (1) /* _stream test_table (pk ) (1 ); */",
		sqldb.NewSQLError(mysqlconn.ERDupEntry, mysqlconn.SSDupKey, "ERROR 1062 (23000): Duplicate entry '2' for key 'PRIMARY'"),
	)
	db.AddQuery(
		"update test_table set val = 1 where pk in (1) /* _stream test_table (pk ) (1 ); */",
		&sqltypes.Result{RowsAffected: 1},
	)
	got, err = qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	want = &sqltypes.Result{
		RowsAffected: 2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
}

func TestQueryExecutorPlanDmlPk(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "update test_table set name = 2 where pk in (1) /* _stream test_table (pk ) (1 ); */"
	want := &sqltypes.Result{}
	db.AddQuery(query, want)
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	txid := newTransaction(tsv)
	qre := newTestQueryExecutor(ctx, tsv, query, txid)
	defer tsv.StopService()
	defer testCommitHelper(t, tsv, qre)
	checkPlanID(t, planbuilder.PlanDMLPK, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
	wantqueries := []string{"update test_table set name = 2 where pk in (1) /* _stream test_table (pk ) (1 ); */"}
	gotqueries := fetchRecordedQueries(qre)
	if !reflect.DeepEqual(gotqueries, wantqueries) {
		t.Errorf("queries: %v, want %v", gotqueries, wantqueries)
	}
}

func TestQueryExecutorPlanDmlMessage(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "update msg set time_acked = 2, time_next = null where id in (1)"
	want := &sqltypes.Result{}
	db.AddQuery("select time_scheduled, id from msg where id in (1) limit 10001 for update", &sqltypes.Result{
		Fields: []*querypb.Field{
			{Type: sqltypes.Int64},
			{Type: sqltypes.Int64},
		},
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{{
			sqltypes.MakeString([]byte("12")),
			sqltypes.MakeString([]byte("1")),
		}},
	})
	db.AddQuery("update msg set time_acked = 2, time_next = null where (time_scheduled = 12 and id = 1) /* _stream msg (time_scheduled id ) (12 1 ); */", want)
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	txid := newTransaction(tsv)
	qre := newTestQueryExecutor(ctx, tsv, query, txid)
	defer tsv.StopService()
	defer testCommitHelper(t, tsv, qre)
	checkPlanID(t, planbuilder.PlanDMLSubquery, qre.plan.PlanID)
	_, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	conn, err := qre.te.txPool.Get(txid, "for test")
	if err != nil {
		t.Fatal(err)
	}
	wantChanged := map[string][]string{"msg": {"1"}}
	if !reflect.DeepEqual(conn.ChangedMessages, wantChanged) {
		t.Errorf("conn.ChangedMessages: %+v, want: %+v", conn.ChangedMessages, wantChanged)
	}
	conn.Recycle()
}

func TestQueryExecutorPlanDmlAutoCommit(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "update test_table set name = 2 where pk in (1) /* _stream test_table (pk ) (1 ); */"
	want := &sqltypes.Result{}
	db.AddQuery(query, want)
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanDMLPK, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
}

func TestQueryExecutorPlanDmlSubQuery(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "update test_table set addr = 3 where name = 1 limit 1000"
	expandedQuery := "select pk from test_table where name = 1 limit 1000 for update"
	want := &sqltypes.Result{}
	db.AddQuery(query, want)
	db.AddQuery(expandedQuery, &sqltypes.Result{
		Fields: []*querypb.Field{
			{Type: sqltypes.Int32},
		},
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{
			{sqltypes.MakeTrusted(sqltypes.Int32, []byte("2"))},
		},
	})
	updateQuery := "update test_table set addr = 3 where pk in (2) /* _stream test_table (pk ) (2 ); */"
	db.AddQuery(updateQuery, want)
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	txid := newTransaction(tsv)
	qre := newTestQueryExecutor(ctx, tsv, query, txid)
	defer tsv.StopService()
	defer testCommitHelper(t, tsv, qre)
	checkPlanID(t, planbuilder.PlanDMLSubquery, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
	wantqueries := []string{updateQuery}
	gotqueries := fetchRecordedQueries(qre)
	if !reflect.DeepEqual(gotqueries, wantqueries) {
		t.Errorf("queries: %v, want %v", gotqueries, wantqueries)
	}
}

func TestQueryExecutorPlanDmlSubQueryAutoCommit(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "update test_table set addr = 3 where name = 1 limit 1000"
	expandedQuery := "select pk from test_table where name = 1 limit 1000 for update"
	want := &sqltypes.Result{}
	db.AddQuery(query, want)
	db.AddQuery(expandedQuery, want)
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanDMLSubquery, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
}

func TestQueryExecutorPlanOtherWithinATransaction(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "show test_table"
	want := &sqltypes.Result{
		Fields: getTestTableFields(),
	}
	db.AddQuery(query, want)
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	txid := newTransaction(tsv)
	qre := newTestQueryExecutor(ctx, tsv, query, txid)
	defer tsv.StopService()
	defer testCommitHelper(t, tsv, qre)
	checkPlanID(t, planbuilder.PlanOther, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	// Clear the flags, they interfere with the diff.
	// FIXME(alainjobart) the new mysqlconn client won't have this issue.
	for _, field := range got.Fields {
		field.Flags = 0
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
	if gotqueries := fetchRecordedQueries(qre); gotqueries != nil {
		t.Errorf("queries: %v, want nil", gotqueries)
	}
}

func TestQueryExecutorPlanPassSelectWithInATransaction(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	fields := []*querypb.Field{
		{Name: "addr", Type: sqltypes.Int32},
	}
	query := "select addr from test_table where pk = 1 limit 1000"
	want := &sqltypes.Result{
		Fields:       fields,
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{
			{sqltypes.MakeTrusted(sqltypes.Int32, []byte("123"))},
		},
	}
	db.AddQuery(query, want)
	db.AddQuery("select addr from test_table where 1 != 1", &sqltypes.Result{
		Fields: fields,
	})
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	txid := newTransaction(tsv)
	qre := newTestQueryExecutor(ctx, tsv, query, txid)
	defer tsv.StopService()
	defer testCommitHelper(t, tsv, qre)
	checkPlanID(t, planbuilder.PlanPassSelect, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	// Clear the flags, they interfere with the diff.
	// FIXME(alainjobart) the new mysqlconn client won't have this issue.
	for _, field := range got.Fields {
		field.Flags = 0
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
	if gotqueries := fetchRecordedQueries(qre); gotqueries != nil {
		t.Errorf("queries: %v, want nil", gotqueries)
	}
}

func TestQueryExecutorPlanPassSelectWithLockOutsideATransaction(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "select * from test_table for update"
	want := &sqltypes.Result{
		Fields: getTestTableFields(),
		Rows:   [][]sqltypes.Value{},
	}
	db.AddQuery(query, want)
	db.AddQuery("select * from test_table where 1 != 1", &sqltypes.Result{
		Fields: getTestTableFields(),
	})
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanSelectLock, qre.plan.PlanID)
	_, err := qre.Execute()
	if err == nil {
		t.Fatal("got: nil, want: error")
	}
	got, ok := err.(*tabletenv.TabletError)
	if !ok {
		t.Fatalf("got: %v, want: *tabletenv.TabletError", err)
	}
	if got.ErrorCode != vtrpcpb.ErrorCode_BAD_INPUT {
		t.Fatalf("got: %s, want: BAD_INPUT", got.ErrorCode)
	}
}

func TestQueryExecutorPlanPassSelect(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "select * from test_table limit 1000"
	want := &sqltypes.Result{
		Fields: getTestTableFields(),
	}
	db.AddQuery(query, want)
	db.AddQuery("select * from test_table where 1 != 1", &sqltypes.Result{
		Fields: getTestTableFields(),
	})
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanPassSelect, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	// Clear the flags, they interfere with the diff.
	// FIXME(alainjobart) the new mysqlconn client won't have this issue.
	for _, field := range got.Fields {
		field.Flags = 0
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got: %v, want: %v", got, want)
	}
}

func TestQueryExecutorPlanSet(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	setQuery := "set unknown_key = 1"
	db.AddQuery(setQuery, &sqltypes.Result{})
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	defer tsv.StopService()
	qre := newTestQueryExecutor(ctx, tsv, setQuery, 0)
	checkPlanID(t, planbuilder.PlanSet, qre.plan.PlanID)
	// Query will be delegated to MySQL and both Fields and Rows should be
	// empty arrays in this case.
	want := &sqltypes.Result{}
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qre.Execute() = %v, want: %v", got, want)
	}

	// Test inside transaction.
	txid := newTransaction(tsv)
	qre = newTestQueryExecutor(ctx, tsv, setQuery, txid)
	got, err = qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qre.Execute() = %v, want: %v", got, want)
	}
	wantqueries := []string{"set unknown_key = 1"}
	gotqueries := fetchRecordedQueries(qre)
	if !reflect.DeepEqual(gotqueries, wantqueries) {
		t.Errorf("queries: %v, want %v", gotqueries, wantqueries)
	}
	testCommitHelper(t, tsv, qre)
	tsv.StopService()
}

func TestQueryExecutorPlanOther(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "show test_table"
	want := &sqltypes.Result{
		Fields: getTestTableFields(),
	}
	db.AddQuery(query, want)
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanOther, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("got: %v, want nil", err)
	}
	// Clear the flags, they interfere with the diff.
	// FIXME(alainjobart) the new mysqlconn client won't have this issue.
	for _, field := range got.Fields {
		field.Flags = 0
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qre.Execute() = %v, want: %v", got, want)
	}
}

func TestQueryExecutorPlanNextval(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	selQuery := "select next_id, cache from seq where id = 0 for update"
	db.AddQuery(selQuery, &sqltypes.Result{
		Fields: []*querypb.Field{
			{Type: sqltypes.Int64},
			{Type: sqltypes.Int64},
		},
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{{
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("1")),
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("3")),
		}},
	})
	updateQuery := "update seq set next_id = 4 where id = 0"
	db.AddQuery(updateQuery, &sqltypes.Result{})
	ctx := context.Background()
	tsv := newTestTabletServer(ctx, enableStrict, db)
	defer tsv.StopService()
	qre := newTestQueryExecutor(ctx, tsv, "select next value from seq", 0)
	checkPlanID(t, planbuilder.PlanNextval, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	want := &sqltypes.Result{
		Fields: []*querypb.Field{{
			Name: "nextval",
			Type: sqltypes.Int64,
		}},
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{{
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("1")),
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qre.Execute() =\n%#v, want:\n%#v", got, want)
	}

	// At this point, NextVal==2, LastVal==4.
	// So, a single value gen should not cause a db access.
	db.DeleteQuery(selQuery)
	qre = newTestQueryExecutor(ctx, tsv, "select next 1 values from seq", 0)
	got, err = qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	want = &sqltypes.Result{
		Fields: []*querypb.Field{{
			Name: "nextval",
			Type: sqltypes.Int64,
		}},
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{{
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("2")),
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qre.Execute() =\n%#v, want:\n%#v", got, want)
	}

	// NextVal==3, LastVal==4
	// Let's try the next 2 values.
	db.AddQuery(selQuery, &sqltypes.Result{
		Fields: []*querypb.Field{
			{Type: sqltypes.Int64},
			{Type: sqltypes.Int64},
		},
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{{
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("4")),
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("3")),
		}},
	})
	updateQuery = "update seq set next_id = 7 where id = 0"
	db.AddQuery(updateQuery, &sqltypes.Result{})
	qre = newTestQueryExecutor(ctx, tsv, "select next 2 values from seq", 0)
	got, err = qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	want = &sqltypes.Result{
		Fields: []*querypb.Field{{
			Name: "nextval",
			Type: sqltypes.Int64,
		}},
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{{
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("3")),
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qre.Execute() =\n%#v, want:\n%#v", got, want)
	}

	// NextVal==5, LastVal==7
	// Let's try jumping a full cache range.
	db.AddQuery(selQuery, &sqltypes.Result{
		Fields: []*querypb.Field{
			{Type: sqltypes.Int64},
			{Type: sqltypes.Int64},
		},
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{{
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("7")),
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("3")),
		}},
	})
	updateQuery = "update seq set next_id = 13 where id = 0"
	db.AddQuery(updateQuery, &sqltypes.Result{})
	qre = newTestQueryExecutor(ctx, tsv, "select next 6 values from seq", 0)
	got, err = qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want nil", err)
	}
	want = &sqltypes.Result{
		Fields: []*querypb.Field{{
			Name: "nextval",
			Type: sqltypes.Int64,
		}},
		RowsAffected: 1,
		Rows: [][]sqltypes.Value{{
			sqltypes.MakeTrusted(sqltypes.Int64, []byte("5")),
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qre.Execute() =\n%#v, want:\n%#v", got, want)
	}
}

func TestQueryExecutorTableAcl(t *testing.T) {
	aclName := fmt.Sprintf("simpleacl-test-%d", rand.Int63())
	tableacl.Register(aclName, &simpleacl.Factory{})
	tableacl.SetDefaultACL(aclName)
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "select * from test_table limit 1000"
	want := &sqltypes.Result{
		Fields: getTestTableFields(),
	}
	db.AddQuery(query, want)
	db.AddQuery("select * from test_table where 1 != 1", &sqltypes.Result{
		Fields: getTestTableFields(),
	})

	username := "u2"
	callerID := &querypb.VTGateCallerID{
		Username: username,
	}
	ctx := callerid.NewContext(context.Background(), nil, callerID)
	config := &tableaclpb.Config{
		TableGroups: []*tableaclpb.TableGroupSpec{{
			Name:                 "group01",
			TableNamesOrPrefixes: []string{"test_table"},
			Readers:              []string{username},
		}},
	}
	if err := tableacl.InitFromProto(config); err != nil {
		t.Fatalf("unable to load tableacl config, error: %v", err)
	}

	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanPassSelect, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("got: %v, want nil", err)
	}
	// Clear the flags, they interfere with the diff.
	// FIXME(alainjobart) the new mysqlconn client won't have this issue.
	for _, field := range got.Fields {
		field.Flags = 0
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qre.Execute() = %v, want: %v", got, want)
	}
}

func TestQueryExecutorTableAclNoPermission(t *testing.T) {
	aclName := fmt.Sprintf("simpleacl-test-%d", rand.Int63())
	tableacl.Register(aclName, &simpleacl.Factory{})
	tableacl.SetDefaultACL(aclName)
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "select * from test_table limit 1000"
	want := &sqltypes.Result{
		Fields: getTestTableFields(),
	}
	db.AddQuery(query, want)
	db.AddQuery("select * from test_table where 1 != 1", &sqltypes.Result{
		Fields: getTestTableFields(),
	})

	username := "u2"
	callerID := &querypb.VTGateCallerID{
		Username: username,
	}
	ctx := callerid.NewContext(context.Background(), nil, callerID)
	config := &tableaclpb.Config{
		TableGroups: []*tableaclpb.TableGroupSpec{{
			Name:                 "group02",
			TableNamesOrPrefixes: []string{"test_table"},
			Readers:              []string{"superuser"},
		}},
	}

	if err := tableacl.InitFromProto(config); err != nil {
		t.Fatalf("unable to load tableacl config, error: %v", err)
	}
	// without enabling Config.StrictTableAcl
	tsv := newTestTabletServer(ctx, enableStrict, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	checkPlanID(t, planbuilder.PlanPassSelect, qre.plan.PlanID)
	got, err := qre.Execute()
	if err != nil {
		t.Fatalf("got: %v, want nil", err)
	}
	// Clear the flags, they interfere with the diff.
	// FIXME(alainjobart) the new mysqlconn client won't have this issue.
	for _, f := range got.Fields {
		f.Flags = 0
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qre.Execute() = %v, want: %v", got, want)
	}
	tsv.StopService()

	// enable Config.StrictTableAcl
	tsv = newTestTabletServer(ctx, enableStrict|enableStrictTableACL, db)
	qre = newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanPassSelect, qre.plan.PlanID)
	// query should fail because current user do not have read permissions
	_, err = qre.Execute()
	if err == nil {
		t.Fatal("got: nil, want: error")
	}
	tabletError, ok := err.(*tabletenv.TabletError)
	if !ok {
		t.Fatalf("got: %v, want: *tabletenv.TabletError", err)
	}
	if tabletError.ErrorCode != vtrpcpb.ErrorCode_PERMISSION_DENIED {
		t.Fatalf("got: %s, want: PERMISSION_DENIED", tabletError.ErrorCode)
	}
}

func TestQueryExecutorTableAclExemptACL(t *testing.T) {
	aclName := fmt.Sprintf("simpleacl-test-%d", rand.Int63())
	tableacl.Register(aclName, &simpleacl.Factory{})
	tableacl.SetDefaultACL(aclName)
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "select * from test_table limit 1000"
	want := &sqltypes.Result{
		Fields:       getTestTableFields(),
		RowsAffected: 0,
		Rows:         [][]sqltypes.Value{},
	}
	db.AddQuery(query, want)
	db.AddQuery("select * from test_table where 1 != 1", &sqltypes.Result{
		Fields: getTestTableFields(),
	})

	username := "u2"
	callerID := &querypb.VTGateCallerID{
		Username: username,
	}
	ctx := callerid.NewContext(context.Background(), nil, callerID)

	config := &tableaclpb.Config{
		TableGroups: []*tableaclpb.TableGroupSpec{{
			Name:                 "group02",
			TableNamesOrPrefixes: []string{"test_table"},
			Readers:              []string{"u1"},
		}},
	}

	if err := tableacl.InitFromProto(config); err != nil {
		t.Fatalf("unable to load tableacl config, error: %v", err)
	}

	// enable Config.StrictTableAcl
	tsv := newTestTabletServer(ctx, enableStrict|enableStrictTableACL, db)
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanPassSelect, qre.plan.PlanID)
	// query should fail because current user do not have read permissions
	_, err := qre.Execute()
	if err == nil {
		t.Fatal("got: nil, want: error")
	}
	tabletError, ok := err.(*tabletenv.TabletError)
	if !ok {
		t.Fatalf("got: %v, want: *tabletenv.TabletError", err)
	}
	if tabletError.ErrorCode != vtrpcpb.ErrorCode_PERMISSION_DENIED {
		t.Fatalf("got: %s, want: PERMISSION_DENIED", tabletError.ErrorCode)
	}
	if !strings.Contains(tabletError.Error(), "table acl error") {
		t.Fatalf("got %s, want tablet errorL table acl error", tabletError.Error())
	}

	// table acl should be ignored since this is an exempt user.
	username = "exempt-acl"
	f, _ := tableacl.GetCurrentAclFactory()
	if tsv.qe.exemptACL, err = f.New([]string{username}); err != nil {
		t.Fatalf("Cannot load exempt ACL for Table ACL: %v", err)
	}
	callerID = &querypb.VTGateCallerID{
		Username: username,
	}
	ctx = callerid.NewContext(context.Background(), nil, callerID)

	qre = newTestQueryExecutor(ctx, tsv, query, 0)
	_, err = qre.Execute()
	if err != nil {
		t.Fatal("qre.Execute: nil, want: error")
	}
}

func TestQueryExecutorTableAclDryRun(t *testing.T) {
	aclName := fmt.Sprintf("simpleacl-test-%d", rand.Int63())
	tableacl.Register(aclName, &simpleacl.Factory{})
	tableacl.SetDefaultACL(aclName)
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "select * from test_table limit 1000"
	want := &sqltypes.Result{
		Fields:       getTestTableFields(),
		RowsAffected: 0,
		Rows:         [][]sqltypes.Value{},
	}
	db.AddQuery(query, want)
	db.AddQuery("select * from test_table where 1 != 1", &sqltypes.Result{
		Fields: getTestTableFields(),
	})

	username := "u2"
	callerID := &querypb.VTGateCallerID{
		Username: username,
	}
	ctx := callerid.NewContext(context.Background(), nil, callerID)

	config := &tableaclpb.Config{
		TableGroups: []*tableaclpb.TableGroupSpec{{
			Name:                 "group02",
			TableNamesOrPrefixes: []string{"test_table"},
			Readers:              []string{"u1"},
		}},
	}

	if err := tableacl.InitFromProto(config); err != nil {
		t.Fatalf("unable to load tableacl config, error: %v", err)
	}

	tableACLStatsKey := strings.Join([]string{
		"test_table",
		"group02",
		planbuilder.PlanPassSelect.String(),
		username,
	}, ".")
	// enable Config.StrictTableAcl
	tsv := newTestTabletServer(ctx, enableStrict|enableStrictTableACL, db)
	tsv.qe.enableTableAclDryRun = true
	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()
	checkPlanID(t, planbuilder.PlanPassSelect, qre.plan.PlanID)
	beforeCount := tabletenv.TableaclPseudoDenied.Counters.Counts()[tableACLStatsKey]
	// query should fail because current user do not have read permissions
	_, err := qre.Execute()
	if err != nil {
		t.Fatalf("qre.Execute() = %v, want: nil", err)
	}
	afterCount := tabletenv.TableaclPseudoDenied.Counters.Counts()[tableACLStatsKey]
	if afterCount-beforeCount != 1 {
		t.Fatalf("table acl pseudo denied count should increase by one. got: %d, want: %d", afterCount, beforeCount+1)
	}
}

func TestQueryExecutorBlacklistQRFail(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "select * from test_table where name = 1 limit 1000"
	expandedQuery := "select pk from test_table use index (`index`) where name = 1 limit 1000"
	expected := &sqltypes.Result{
		Fields: getTestTableFields(),
	}
	db.AddQuery(query, expected)
	db.AddQuery(expandedQuery, expected)

	db.AddQuery("select * from test_table where 1 != 1", &sqltypes.Result{
		Fields: getTestTableFields(),
	})

	bannedAddr := "127.0.0.1"
	bannedUser := "u2"

	alterRule := NewQueryRule("disable update", "disable update", QRFail)
	alterRule.SetIPCond(bannedAddr)
	alterRule.SetUserCond(bannedUser)
	alterRule.SetQueryCond("select.*")
	alterRule.AddPlanCond(planbuilder.PlanPassSelect)
	alterRule.AddTableCond("test_table")

	rulesName := "blacklistedRulesQRFail"
	rules := NewQueryRules()
	rules.Add(alterRule)

	callInfo := &fakecallinfo.FakeCallInfo{
		Remote: bannedAddr,
		User:   bannedUser,
	}
	ctx := callinfo.NewContext(context.Background(), callInfo)
	tsv := newTestTabletServer(ctx, enableStrict, db)
	tsv.qe.schemaInfo.queryRuleSources.UnRegisterQueryRuleSource(rulesName)
	tsv.qe.schemaInfo.queryRuleSources.RegisterQueryRuleSource(rulesName)
	defer tsv.qe.schemaInfo.queryRuleSources.UnRegisterQueryRuleSource(rulesName)

	if err := tsv.qe.schemaInfo.queryRuleSources.SetRules(rulesName, rules); err != nil {
		t.Fatalf("failed to set rule, error: %v", err)
	}

	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()

	checkPlanID(t, planbuilder.PlanPassSelect, qre.plan.PlanID)
	// execute should fail because query has been blacklisted
	_, err := qre.Execute()
	if err == nil {
		t.Fatal("got: nil, want: error")
	}
	got, ok := err.(*tabletenv.TabletError)
	if !ok {
		t.Fatalf("got: %v, want: *tabletenv.TabletError", err)
	}
	if got.ErrorCode != vtrpcpb.ErrorCode_BAD_INPUT {
		t.Fatalf("got: %s, want: BAD_INPUT", got.ErrorCode)
	}
}

func TestQueryExecutorBlacklistQRRetry(t *testing.T) {
	db := setUpQueryExecutorTest(t)
	defer db.Close()
	query := "select * from test_table where name = 1 limit 1000"
	expandedQuery := "select pk from test_table use index (`index`) where name = 1 limit 1000"
	expected := &sqltypes.Result{
		Fields: getTestTableFields(),
	}
	db.AddQuery(query, expected)
	db.AddQuery(expandedQuery, expected)

	db.AddQuery("select * from test_table where 1 != 1", &sqltypes.Result{
		Fields: getTestTableFields(),
	})

	bannedAddr := "127.0.0.1"
	bannedUser := "x"

	alterRule := NewQueryRule("disable update", "disable update", QRFailRetry)
	alterRule.SetIPCond(bannedAddr)
	alterRule.SetUserCond(bannedUser)
	alterRule.SetQueryCond("select.*")
	alterRule.AddPlanCond(planbuilder.PlanPassSelect)
	alterRule.AddTableCond("test_table")

	rulesName := "blacklistedRulesQRRetry"
	rules := NewQueryRules()
	rules.Add(alterRule)

	callInfo := &fakecallinfo.FakeCallInfo{
		Remote: bannedAddr,
		User:   bannedUser,
	}
	ctx := callinfo.NewContext(context.Background(), callInfo)
	tsv := newTestTabletServer(ctx, enableStrict, db)
	tsv.qe.schemaInfo.queryRuleSources.UnRegisterQueryRuleSource(rulesName)
	tsv.qe.schemaInfo.queryRuleSources.RegisterQueryRuleSource(rulesName)
	defer tsv.qe.schemaInfo.queryRuleSources.UnRegisterQueryRuleSource(rulesName)

	if err := tsv.qe.schemaInfo.queryRuleSources.SetRules(rulesName, rules); err != nil {
		t.Fatalf("failed to set rule, error: %v", err)
	}

	qre := newTestQueryExecutor(ctx, tsv, query, 0)
	defer tsv.StopService()

	checkPlanID(t, planbuilder.PlanPassSelect, qre.plan.PlanID)
	_, err := qre.Execute()
	if err == nil {
		t.Fatal("got: nil, want: error")
	}
	got, ok := err.(*tabletenv.TabletError)
	if !ok {
		t.Fatalf("got: %v, want: *tabletenv.TabletError", err)
	}
	if got.ErrorCode != vtrpcpb.ErrorCode_QUERY_NOT_SERVED {
		t.Fatalf("got: %s, want: QUERY_NOT_SERVED", got.ErrorCode)
	}
}

type executorFlags int64

const (
	noFlags      executorFlags = 0
	enableStrict               = 1 << iota
	enableStrictTableACL
	smallTxPool
	noTwopc
	shortTwopcAge
)

// newTestQueryExecutor uses a package level variable testTabletServer defined in tabletserver_test.go
func newTestTabletServer(ctx context.Context, flags executorFlags, db *fakesqldb.DB) *TabletServer {
	randID := rand.Int63()
	tabletenv.Config = tabletenv.DefaultQsConfig
	config := &tabletenv.Config
	config.DebugURLPrefix = fmt.Sprintf("/debug-%d-", randID)
	config.PoolNamePrefix = fmt.Sprintf("Pool-%d-", randID)
	config.PoolSize = 100
	if flags&smallTxPool > 0 {
		config.TransactionCap = 3
	} else {
		config.TransactionCap = 100
	}
	config.EnableAutoCommit = true

	if flags&enableStrict > 0 {
		config.StrictMode = true
	} else {
		config.StrictMode = false
	}
	if flags&enableStrictTableACL > 0 {
		config.StrictTableAcl = true
	} else {
		config.StrictTableAcl = false
	}
	if flags&noTwopc > 0 {
		config.TwoPCEnable = false
	} else {
		config.TwoPCEnable = true
	}
	config.TwoPCCoordinatorAddress = "fake"
	if flags&shortTwopcAge > 0 {
		config.TwoPCAbandonAge = 0.5
	} else {
		config.TwoPCAbandonAge = 10
	}
	tsv := NewTabletServer()
	testUtils := newTestUtils()
	dbconfigs := testUtils.newDBConfigs(db)
	target := querypb.Target{TabletType: topodatapb.TabletType_MASTER}
	tsv.StartService(target, dbconfigs, testUtils.newMysqld(&dbconfigs))
	return tsv
}

func newTransaction(tsv *TabletServer) int64 {
	transactionID, err := tsv.Begin(context.Background(), &tsv.target)
	if err != nil {
		panic(fmt.Errorf("failed to start a transaction: %v", err))
	}
	return transactionID
}

func newTestQueryExecutor(ctx context.Context, tsv *TabletServer, sql string, txID int64) *QueryExecutor {
	logStats := tabletenv.NewLogStats(ctx, "TestQueryExecutor")
	plan, err := tsv.qe.schemaInfo.GetPlan(ctx, logStats, sql)
	if err != nil {
		panic(err)
	}
	return &QueryExecutor{
		ctx:           ctx,
		query:         sql,
		bindVars:      make(map[string]interface{}),
		transactionID: txID,
		plan:          plan,
		logStats:      logStats,
		qe:            tsv.qe,
		te:            tsv.te,
		messager:      tsv.messager,
	}
}

func testCommitHelper(t *testing.T, tsv *TabletServer, queryExecutor *QueryExecutor) {
	if err := tsv.Commit(queryExecutor.ctx, &tsv.target, queryExecutor.transactionID); err != nil {
		t.Fatalf("failed to commit transaction: %d, err: %v", queryExecutor.transactionID, err)
	}
}

func setUpQueryExecutorTest(t *testing.T) *fakesqldb.DB {
	db := fakesqldb.New(t)
	initQueryExecutorTestDB(db)
	return db
}

func initQueryExecutorTestDB(db *fakesqldb.DB) {
	for query, result := range getQueryExecutorSupportedQueries() {
		db.AddQuery(query, result)
	}
}

func fetchRecordedQueries(qre *QueryExecutor) []string {
	conn, err := qre.te.txPool.Get(qre.transactionID, "for query")
	if err != nil {
		panic(err)
	}
	defer conn.Recycle()
	return conn.Queries
}

func getTestTableFields() []*querypb.Field {
	return []*querypb.Field{
		{Name: "pk", Type: sqltypes.Int32},
		{Name: "name", Type: sqltypes.Int32},
		{Name: "addr", Type: sqltypes.Int32},
	}
}

func checkPlanID(
	t *testing.T,
	expectedPlanID planbuilder.PlanType,
	actualPlanID planbuilder.PlanType) {
	if expectedPlanID != actualPlanID {
		t.Fatalf("expect to get PlanID: %s, but got %s",
			expectedPlanID.String(), actualPlanID.String())
	}
}

func getQueryExecutorSupportedQueries() map[string]*sqltypes.Result {
	return map[string]*sqltypes.Result{
		// queries for twopc
		sqlTurnoffBinlog:                                  {},
		fmt.Sprintf(sqlCreateSidecarDB, "`_vt`"):          {},
		fmt.Sprintf(sqlDropLegacy1, "`_vt`"):              {},
		fmt.Sprintf(sqlDropLegacy2, "`_vt`"):              {},
		fmt.Sprintf(sqlDropLegacy3, "`_vt`"):              {},
		fmt.Sprintf(sqlDropLegacy4, "`_vt`"):              {},
		fmt.Sprintf(sqlCreateTableRedoState, "`_vt`"):     {},
		fmt.Sprintf(sqlCreateTableRedoStatement, "`_vt`"): {},
		fmt.Sprintf(sqlCreateTableDTState, "`_vt`"):       {},
		fmt.Sprintf(sqlCreateTableDTParticipant, "`_vt`"): {},
		// queries for schema info
		"select unix_timestamp()": {
			Fields: []*querypb.Field{{
				Type: sqltypes.Uint64,
			}},
			RowsAffected: 1,
			Rows: [][]sqltypes.Value{
				{sqltypes.MakeTrusted(sqltypes.Int32, []byte("1427325875"))},
			},
		},
		"select @@global.sql_mode": {
			Fields: []*querypb.Field{{
				Type: sqltypes.VarChar,
			}},
			RowsAffected: 1,
			Rows: [][]sqltypes.Value{
				{sqltypes.MakeString([]byte("STRICT_TRANS_TABLES"))},
			},
		},
		"select @@autocommit": {
			Fields: []*querypb.Field{{
				Type: sqltypes.Uint64,
			}},
			RowsAffected: 1,
			Rows: [][]sqltypes.Value{
				{sqltypes.MakeString([]byte("1"))},
			},
		},
		mysqlconn.BaseShowTables: {
			Fields:       mysqlconn.BaseShowTablesFields,
			RowsAffected: 3,
			Rows: [][]sqltypes.Value{
				mysqlconn.BaseShowTablesRow("test_table", false, ""),
				mysqlconn.BaseShowTablesRow("seq", false, "vitess_sequence"),
				mysqlconn.BaseShowTablesRow("msg", false, "vitess_message,vt_ack_wait=30,vt_purge_after=120,vt_batch_size=1,vt_cache_size=10,vt_poller_interval=30"),
			},
		},
		"select * from test_table where 1 != 1": {
			Fields: []*querypb.Field{{
				Name: "pk",
				Type: sqltypes.Int32,
			}, {
				Name: "name",
				Type: sqltypes.Int32,
			}, {
				Name: "addr",
				Type: sqltypes.Int32,
			}},
		},
		"describe test_table": {
			Fields:       mysqlconn.DescribeTableFields,
			RowsAffected: 3,
			Rows: [][]sqltypes.Value{
				mysqlconn.DescribeTableRow("pk", "int(11)", false, "PRI", "0"),
				mysqlconn.DescribeTableRow("name", "int(11)", false, "", "0"),
				mysqlconn.DescribeTableRow("addr", "int(11)", false, "", "0"),
			},
		},
		// for SplitQuery because it needs a primary key column
		"show index from test_table": {
			Fields:       mysqlconn.ShowIndexFromTableFields,
			RowsAffected: 2,
			Rows: [][]sqltypes.Value{
				mysqlconn.ShowIndexFromTableRow("test_table", true, "PRIMARY", 1, "pk", false),
				mysqlconn.ShowIndexFromTableRow("test_table", false, "index", 1, "name", true),
			},
		},
		"begin":  {},
		"commit": {},
		mysqlconn.BaseShowTablesForTable("test_table"): {
			Fields:       mysqlconn.BaseShowTablesFields,
			RowsAffected: 1,
			Rows: [][]sqltypes.Value{
				mysqlconn.BaseShowTablesRow("test_table", false, ""),
			},
		},
		"rollback": {},
		"select * from seq where 1 != 1": {
			Fields: []*querypb.Field{{
				Name: "id",
				Type: sqltypes.Int32,
			}, {
				Name: "next_id",
				Type: sqltypes.Int64,
			}, {
				Name: "cache",
				Type: sqltypes.Int64,
			}, {
				Name: "increment",
				Type: sqltypes.Int64,
			}},
		},
		"describe seq": {
			Fields:       mysqlconn.DescribeTableFields,
			RowsAffected: 4,
			Rows: [][]sqltypes.Value{
				mysqlconn.DescribeTableRow("id", "int(11)", false, "PRI", "0"),
				mysqlconn.DescribeTableRow("next_id", "bigint(20)", false, "", "0"),
				mysqlconn.DescribeTableRow("cache", "bigint(20)", false, "", "0"),
				mysqlconn.DescribeTableRow("increment", "bigint(20)", false, "", "0"),
			},
		},
		"show index from seq": {
			Fields:       mysqlconn.ShowIndexFromTableFields,
			RowsAffected: 1,
			Rows: [][]sqltypes.Value{
				mysqlconn.ShowIndexFromTableRow("seq", true, "PRIMARY", 1, "id", false),
			},
		},
		mysqlconn.BaseShowTablesForTable("seq"): {
			Fields:       mysqlconn.BaseShowTablesFields,
			RowsAffected: 1,
			Rows: [][]sqltypes.Value{
				mysqlconn.BaseShowTablesRow("seq", false, "vitess_sequence"),
			},
		},
		"select * from msg where 1 != 1": {
			Fields: []*querypb.Field{{
				Name: "time_scheduled",
				Type: sqltypes.Int32,
			}, {
				Name: "id",
				Type: sqltypes.Int64,
			}, {
				Name: "time_next",
				Type: sqltypes.Int64,
			}, {
				Name: "epoch",
				Type: sqltypes.Int64,
			}, {
				Name: "time_created",
				Type: sqltypes.Int64,
			}, {
				Name: "time_acked",
				Type: sqltypes.Int64,
			}, {
				Name: "message",
				Type: sqltypes.Int64,
			}},
		},
		"describe msg": {
			Fields:       mysqlconn.DescribeTableFields,
			RowsAffected: 7,
			Rows: [][]sqltypes.Value{
				mysqlconn.DescribeTableRow("time_scheduled", "int(11)", false, "PRI", "0"),
				mysqlconn.DescribeTableRow("id", "bigint(20)", false, "PRI", "0"),
				mysqlconn.DescribeTableRow("time_next", "bigint(20)", false, "", "0"),
				mysqlconn.DescribeTableRow("epoch", "bigint(20)", false, "", "0"),
				mysqlconn.DescribeTableRow("time_created", "bigint(20)", false, "", "0"),
				mysqlconn.DescribeTableRow("time_acked", "bigint(20)", false, "", "0"),
				mysqlconn.DescribeTableRow("message", "bigint(20)", false, "", "0"),
			},
		},
		"show index from msg": {
			Fields:       mysqlconn.ShowIndexFromTableFields,
			RowsAffected: 1,
			Rows: [][]sqltypes.Value{
				mysqlconn.ShowIndexFromTableRow("msg", true, "PRIMARY", 1, "time_scheduled", false),
				mysqlconn.ShowIndexFromTableRow("msg", true, "PRIMARY", 2, "id", false),
			},
		},
		mysqlconn.BaseShowTablesForTable("msg"): {
			Fields:       mysqlconn.BaseShowTablesFields,
			RowsAffected: 1,
			Rows: [][]sqltypes.Value{
				mysqlconn.BaseShowTablesRow("test_table", false, "vitess_message,vt_ack_wait=30,vt_purge_after=120,vt_batch_size=1,vt_cache_size=10,vt_poller_interval=30"),
			},
		},
		fmt.Sprintf(sqlReadAllRedo, "`_vt`", "`_vt`"): {},
	}
}
