package mgodo

import (
	"reflect"
	"time"

	"github.com/globalsign/mgo"
	"github.com/globalsign/mgo/bson"
)

//Do wrap all common functions
type Do struct {
	model         interface{}
	session       *mgo.Session
	collection    *mgo.Collection
	logCollection *mgo.Collection // for change log
	Query         bson.M
	Sort          []string
	Skip          int
	Limit         int
	Operator      string
	Reason        string
}

//NewDo initiate with input model and mgo session
func NewDo(s *mgo.Session, dbName string, model interface{}, operator, reason string) *Do {
	do := &Do{model: model, session: s}
	do.collection = Collection(s, dbName, model)
	do.logCollection = Collection(s, dbName, "ChangeLog")
	do.Operator = operator
	do.Reason = reason
	return do
}

// Collection conduct mgo.Collection
func Collection(s *mgo.Session, dbName string, m interface{}) *mgo.Collection {
	cName := getModelName(m)
	return s.DB(dbName).C(cName)
}

//getModelName reflect string name from model
func getModelName(m interface{}) string {
	var c string
	switch m.(type) {
	case string:
		c = m.(string)
	default:
		typ := reflect.TypeOf(m)
		if typ.Kind() == reflect.Ptr {
			typ = typ.Elem()
		}
		c = typ.Name()
	}
	return c
}

//Create, generate objectId, upsert record with CreatedAt as Now
func (m *Do) Create() error {
	//generate new object Id
	id := reflect.ValueOf(m.model).Elem().FieldByName("Id")
	id.Set(reflect.ValueOf(bson.NewObjectId()))
	x := reflect.ValueOf(m.model).Elem().FieldByName("CreatedAt")
	x.Set(reflect.ValueOf(time.Now()))
	by := reflect.ValueOf(m.model).Elem().FieldByName("CreatedBy")
	by.Set(reflect.ValueOf(m.Operator))
	_, err := m.collection.Upsert(bson.M{"_id": id.Interface()}, bson.M{"$set": m.model})
	return err
}

//Save method, upsert record with UpdatedAt as now
func (m *Do) Save() error {
	id := reflect.ValueOf(m.model).Elem().FieldByName("Id")
	x := reflect.ValueOf(m.model).Elem().FieldByName("UpdatedAt")
	x.Set(reflect.ValueOf(time.Now()))
	by := reflect.ValueOf(m.model).Elem().FieldByName("UpdatedBy")
	by.Set(reflect.ValueOf(m.Operator))
	_, err := m.collection.Upsert(bson.M{"_id": id.Interface()}, bson.M{"$set": m.model})
	return err
}

//SaveWithLog save record and inset a new changelog record
func (m *Do) SaveWithLog() error {
	var err error
	err = m.Save()
	if err != nil {
		return err
	}
	err = m.saveLog(UPDATE)
	if err != nil {
		return err
	}
	return nil
}

//Erase is hard delete
func (m *Do) Erase() error {
	//hard delete record
	id := reflect.ValueOf(m.model).Elem().FieldByName("Id")
	err := m.collection.RemoveId(id.Interface())
	return err
}

//EraseWithLog, hard delete record and insert a chagnelog
func (m *Do) EraseWithLog() error {
	// Save log
	err := m.saveLog(ERASE)
	if err != nil {
		return err
	}

	// hard delete record
	err = m.Erase()
	return err
}

// Delete is softe delete
func (m *Do) Delete() error {
	id := reflect.ValueOf(m.model).Elem().FieldByName("Id")
	x := reflect.ValueOf(m.model).Elem().FieldByName("RemovedAt")
	x.Set(reflect.ValueOf(time.Now()))
	by := reflect.ValueOf(m.model).Elem().FieldByName("RemovedBy")
	by.Set(reflect.ValueOf(m.Operator))
	removed := reflect.ValueOf(m.model).Elem().FieldByName("IsRemoved")
	removed.Set(reflect.ValueOf(true))

	_, err := m.collection.Upsert(bson.M{"_id": id.Interface()}, bson.M{"$set": m.model})
	return err
}

//DeleteWithLog
func (m *Do) DeleteWithLog() error {
	err := m.saveLog(DELETE)
	if err != nil {
		return err
	}
	err = m.Delete()
	if err != nil {
		return err
	}
	return nil

}

//SaveWithLog
func (m *Do) saveLog(operation string) error {
	//read current record
	var record interface{}
	recordId := reflect.ValueOf(m.model).Elem().FieldByName("Id").Interface().(bson.ObjectId)
	err := m.collection.FindId(recordId).One(&record)
	if err != nil {
		return err
	}

	cl := new(ChangeLog)
	cl.Id = bson.NewObjectId()
	cl.CreatedBy = m.Operator
	cl.CreatedAt = time.Now()
	cl.ChangeReason = m.Reason
	cl.Operation = operation
	cl.ModelObjId = recordId
	cl.ModelName = getModelName(m.model)
	cl.ModelValue = record
	_, err = m.logCollection.Upsert(bson.M{"_id": cl.Id}, bson.M{"$set": cl})
	return err
}

// ---------- General mgo functions -----------

//GenQuery export mgo.Query for further query chain
func (m *Do) Q() *mgo.Query {
	return m.findQ()
}

//findQ conduct mgo.Query, skip IsRemoved: true
func (m *Do) findQ() *mgo.Query {
	var query *mgo.Query
	//do not query removed value
	rmQ := []interface{}{bson.M{"is_removed": bson.M{"$ne": true}}, bson.M{"IsRemoved": bson.M{"$ne": true}}}
	if m.Query != nil {
		if v, found := m.Query["$and"]; !found {
			m.Query["$and"] = rmQ
		} else {
			m.Query["$and"] = append(v.([]interface{}), rmQ...)
		}
	} else {
		m.Query = bson.M{"$and": rmQ}
	}

	query = m.collection.Find(m.Query)
	//sort
	if m.Sort != nil {
		query = query.Sort(m.Sort...)
	} else {
		query = query.Sort("-UpdatedAt", "-CreatedAt")
	}

	//skip
	if m.Skip != 0 {
		query = query.Skip(m.Skip)
	}

	//limit
	if m.Limit != 0 {
		query = query.Limit(m.Limit)
	}
	return query
}

//findByIdQ, skip IsRemoved:true
func (m *Do) findByIdQ() *mgo.Query {
	id := reflect.ValueOf(m.model).Elem().FieldByName("Id").Interface()
	m.Query = bson.M{"_id": id}
	return m.findQ()
}

//Count
func (m *Do) Count() int64 {
	query := m.findQ()
	count, _ := query.Count()
	return int64(count)
}

//---------retrieve functions
// FindAll except removed, i is interface address
func (m *Do) FindAll(i interface{}) error {
	query := m.findQ()
	err := query.All(i)
	return err
}

//Get will retrieve by _id
func (m *Do) Get() error {
	query := m.findByIdQ()
	err := query.One(m.model)
	return err
}

//GetByQ get first one based on query, model will be updated
func (m *Do) GetByQ() error {
	query := m.findQ()
	err := query.One(m.model)
	return err
}

//Select query and select columns
func (m *Do) FindWithSelect(i interface{}, cols []string) error {
	sCols := bson.M{}
	for _, v := range cols {
		sCols[v] = 1
	}
	query := m.findQ().Select(sCols)
	err := query.All(i)
	return err
}

//Distinct
func (m *Do) Distinct(key string, i interface{}) error {
	err := m.findQ().Distinct(key, i)
	return err
}

//GetWithSelect, limit cols
func (m *Do) GetWithSelect(cols []string) error {
	sCols := bson.M{}
	for _, v := range cols {
		sCols[v] = 1
	}
	query := m.findByIdQ().Select(sCols)
	err := query.One(m.model)
	return err
}

//Erase all is hard Delete with raw condition (no predefined skip IsRemoved:true)
func (m *Do) EraseAll() error {
	_, err := m.collection.RemoveAll(m.Query)
	return err
}