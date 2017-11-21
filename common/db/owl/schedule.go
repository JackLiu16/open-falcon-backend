package owl

import (
	"fmt"
	"time"

	"database/sql"

	cdb "github.com/Cepave/open-falcon-backend/common/db"
	sqlxExt "github.com/Cepave/open-falcon-backend/common/db/sqlx"
	"github.com/jmoiron/sqlx"
	"github.com/satori/go.uuid"
)

type Schedule struct {
	Name    string
	Timeout int
	Uuid    uuid.UUID
}

type OwlSchedule struct {
	Id             int       `db:"sch_id"`
	Name           string    `db:"sch_name"`
	Lock           byte      `db:"sch_lock"`
	LastUpdateTime time.Time `db:"sch_modify_time"`
}

type OwlScheduleLog struct {
	Uuid      cdb.DbUuid `db:"sl_uuid"`
	SchId     int        `db:"sl_sch_id"`
	StartTime time.Time  `db:"sl_start_time"`
	EndTime   time.Time  `db:"sl_end_time"`
	Timeout   int        `db:"sl_timeout"`
	Status    byte       `db:"sl_status"`
	Message   string     `db:"sl_message"`
}

var insertSql = `
	INSERT INTO owl_schedule_log(
		sl_uuid, sl_sch_id,
		sl_start_time, sl_timeout, sl_status
	)
	VALUES (:uuid, :schid, :starttime, :timeout, :status)
`

type TimeOutOfSchedule struct {
	Name          string
	AcquiredTime  time.Time
	LastStartTime time.Time
	Timeout       int
}

func (t *TimeOutOfSchedule) Error() string {
	return fmt.Sprintf("%s error: from %s to %s less than %ds", t.Name,
		t.LastStartTime.Format(time.RFC3339),
		t.AcquiredTime.Format(time.RFC3339),
		t.Timeout)
}

type LockStatus byte

const (
	FREE LockStatus = iota
	LOCKED
)

type TaskStatus byte

const (
	DONE TaskStatus = iota
	RUN
	FAIL
	TIMEOUT
)

func AcquireLock(schedule *Schedule) error {
	txProcessor := &txAcquireLock{schedule, nil}
	DbFacade.SqlxDbCtrl.InTx(txProcessor)
	return txProcessor.timeoutError
}

type txAcquireLock struct {
	schedule     *Schedule
	timeoutError *TimeOutOfSchedule
}

func (ack *txAcquireLock) InTx(tx *sqlx.Tx) cdb.TxFinale {

	now := time.Now()

	/**
	 * Lock table
	 */
	ret := ack.selectOrInsertLock(tx, now)
	// The previous task is not timeout()
	if ret.isLocked() && ack.notTimeout(tx, now) {
		return cdb.TxCommit
	}

	if !ack.successUpdateLock(tx, now) {
		return cdb.TxRollback
	}
	// :~)

	/**
	 * Log table
	 */
	generatedUuid := uuid.NewV4()
	r := sqlxExt.ToTxExt(tx).NamedExec(insertSql,
		map[string]interface{}{
			"uuid":      generatedUuid,
			"schid":     ret.Id,
			"starttime": now,
			"timeout":   ack.schedule.Timeout,
			"status":    RUN,
		},
	)
	if !isCorrectRowsAffected(r, 1) {
		return cdb.TxRollback
	}

	ack.schedule.Uuid = generatedUuid
	// :~)

	return cdb.TxCommit
}

type scheduleLock struct {
	Id   int  `db:"sch_id"`
	Lock byte `db:"sch_lock"`
}

func (sck *scheduleLock) isLocked() bool {
	return sck.Lock == byte(LOCKED)
}

func (ack *txAcquireLock) selectOrInsertLock(tx *sqlx.Tx, now time.Time) (ret scheduleLock) {
	name := ack.schedule.Name
	exist := sqlxExt.ToTxExt(tx).GetOrNoRow(&ret, `
		SELECT sch_id, sch_lock
		FROM owl_schedule
		WHERE sch_name = ?
		FOR UPDATE
	`, name)

	if !exist {
		r := tx.MustExec(`
			INSERT INTO owl_schedule(
				sch_name,
				sch_lock, sch_modify_time
			)
			VALUES (?, 0, ?)
		`, name, now)
		ret.Id = int(cdb.ToResultExt(r).LastInsertId())
		ret.Lock = byte(FREE)
	}

	return
}

func (ack *txAcquireLock) successUpdateLock(tx *sqlx.Tx, now time.Time) bool {
	r := tx.MustExec(`
		UPDATE owl_schedule
		SET sch_lock = 1,
			sch_modify_time = ?
		WHERE sch_name = ?
	`, now, ack.schedule.Name)
	return isCorrectRowsAffected(r, 1)
}

func (ack *txAcquireLock) notTimeout(tx *sqlx.Tx, now time.Time) bool {
	ret := struct {
		StartTime time.Time `db:"sl_start_time"`
		Timeout   int       `db:"sl_timeout"`
	}{}
	sqlxExt.ToTxExt(tx).Get(&ret, `
		SELECT sl.sl_start_time, sl.sl_timeout
		FROM owl_schedule sch
		LEFT JOIN owl_schedule_log sl
		ON sch.sch_id = sl.sl_sch_id
		WHERE sch.sch_name = ?
		ORDER BY sl.sl_start_time DESC
		LIMIT 1
	`, ack.schedule.Name)

	shouldLocked := now.Sub(ret.StartTime) <= time.Duration(ret.Timeout)*time.Second
	if shouldLocked {
		ack.timeoutError = &TimeOutOfSchedule{
			Name:          "Schedule locked",
			LastStartTime: ret.StartTime,
			AcquiredTime:  now,
			Timeout:       ret.Timeout,
		}
	}
	return shouldLocked
}

func isCorrectRowsAffected(r sql.Result, expectRowsAffected int64) bool {
	return cdb.ToResultExt(r).RowsAffected() == expectRowsAffected
}