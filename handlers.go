/*
 * Copyright 2018. bigpigeon. All rights reserved.
 * Use of this source code is governed by a MIT style
 * license that can be found in the LICENSE file.
 */

package toyorm

import (
	"errors"
	"fmt"
	"reflect"
	"time"
)

func HandlerPreloadInsertOrSave(option string) func(*Context) error {
	return func(ctx *Context) error {
		for fieldName, preload := range ctx.Brick.BelongToPreload {
			mainField, subField := preload.RelationField, preload.SubModel.GetOnePrimary()
			preloadBrick := ctx.Brick.MapPreloadBrick[fieldName]
			subRecords := MakeRecordsWithElem(preload.SubModel, ctx.Result.Records.GetFieldAddressType(fieldName))

			// map[i]=>j [i]record.SubData -> [j]subRecord
			bindMap := map[int]int{}
			for i, record := range ctx.Result.Records.GetRecords() {
				if ctx.Brick.ignoreModeSelector[ModePreload].Ignore(record.Field(fieldName)) == false {
					bindMap[i] = subRecords.Len()
					subRecords.Add(record.FieldAddress(fieldName))
				}
			}
			subCtx := preloadBrick.GetContext(option, subRecords)
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err := subCtx.Next(); err != nil {
				return err
			}
			// set model relation field
			ctx.Result.SimpleRelation[fieldName] = map[int]int{}
			for i, record := range ctx.Result.Records.GetRecords() {
				if j, ok := bindMap[i]; ok {
					subRecord := subRecords.GetRecord(j)
					record.SetField(mainField.Name(), subRecord.Field(subField.Name()))
					ctx.Result.SimpleRelation[fieldName][j] = i
				}
			}

		}

		if err := ctx.Next(); err != nil {
			return err
		}
		for fieldName, preload := range ctx.Brick.OneToOnePreload {
			preloadBrick := ctx.Brick.MapPreloadBrick[fieldName]
			mainPos, subPos := preload.Model.GetOnePrimary(), preload.RelationField
			subRecords := MakeRecordsWithElem(preload.SubModel, ctx.Result.Records.GetFieldAddressType(fieldName))
			// set sub model relation field
			ctx.Result.SimpleRelation[fieldName] = map[int]int{}
			for i, record := range ctx.Result.Records.GetRecords() {
				if ctx.Brick.ignoreModeSelector[ModePreload].Ignore(record.Field(fieldName)) == false {
					// it means relation field, result[j].LastInsertId() is id value
					subRecord := subRecords.Add(record.FieldAddress(fieldName))
					ctx.Result.SimpleRelation[fieldName][subRecords.Len()-1] = i
					if primary := record.Field(mainPos.Name()); primary.IsValid() {
						subRecord.SetField(subPos.Name(), primary)
					} else {
						panic("relation field not set")
					}
				}
			}
			subCtx := preloadBrick.GetContext(option, subRecords)
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err := subCtx.Next(); err != nil {
				return err
			}

		}

		// one to many
		for fieldName, preload := range ctx.Brick.OneToManyPreload {
			preloadBrick := ctx.Brick.MapPreloadBrick[fieldName]
			mainField, subField := preload.Model.GetOnePrimary(), preload.RelationField
			elemAddressType := reflect.PtrTo(LoopTypeIndirect(ctx.Result.Records.GetFieldType(fieldName)).Elem())
			subRecords := MakeRecordsWithElem(preload.SubModel, elemAddressType)
			// set sub model relation field
			ctx.Result.MultipleRelation[fieldName] = map[int]Pair{}
			for i, record := range ctx.Result.Records.GetRecords() {
				if primary := record.Field(mainField.Name()); primary.IsValid() {
					rField := LoopIndirect(record.Field(fieldName))
					for subi := 0; subi < rField.Len(); subi++ {
						subRecord := subRecords.Add(rField.Index(subi).Addr())
						ctx.Result.MultipleRelation[fieldName][subRecords.Len()-1] = Pair{i, subi}
						subRecord.SetField(subField.Name(), primary)
					}
				} else {
					return errors.New("some records have not primary")
				}
			}
			subCtx := preloadBrick.GetContext(option, subRecords)
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err := subCtx.Next(); err != nil {
				return err
			}
		}
		// many to many
		for fieldName, preload := range ctx.Brick.ManyToManyPreload {
			subBrick := ctx.Brick.MapPreloadBrick[fieldName]
			middleBrick := NewToyBrick(ctx.Brick.Toy, preload.MiddleModel).CopyStatus(ctx.Brick)

			mainField, subField := preload.Model.GetOnePrimary(), preload.SubModel.GetOnePrimary()
			elemAddressType := reflect.PtrTo(LoopTypeIndirect(ctx.Result.Records.GetFieldType(fieldName)).Elem())
			subRecords := MakeRecordsWithElem(preload.SubModel, elemAddressType)

			ctx.Result.MultipleRelation[fieldName] = map[int]Pair{}
			for i, record := range ctx.Result.Records.GetRecords() {
				rField := LoopIndirect(record.Field(fieldName))
				for subi := 0; subi < rField.Len(); subi++ {
					subRecords.Add(rField.Index(subi).Addr())
					ctx.Result.MultipleRelation[fieldName][subRecords.Len()-1] = Pair{i, subi}
				}
			}
			subCtx := subBrick.GetContext(option, subRecords)
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err := subCtx.Next(); err != nil {
				return err
			}

			middleRecords := MakeRecordsWithElem(middleBrick.Model, middleBrick.Model.ReflectType)
			// use to calculate what sub records belong for
			offset := 0
			for _, record := range ctx.Result.Records.GetRecords() {
				primary := record.Field(mainField.Name())
				primary.IsValid()
				if primary.IsValid() == false {
					return errors.New("some records have not primary")
				}
				rField := LoopIndirect(record.Field(fieldName))
				for subi := 0; subi < rField.Len(); subi++ {
					subRecord := subRecords.GetRecord(subi + offset)
					subPrimary := subRecord.Field(subField.Name())
					if subPrimary.IsValid() == false {
						return errors.New("some records have not primary")
					}
					middleRecord := NewRecord(middleBrick.Model, reflect.New(middleBrick.Model.ReflectType).Elem())
					middleRecord.SetField(preload.RelationField.Name(), primary)
					middleRecord.SetField(preload.SubRelationField.Name(), subPrimary)
					middleRecords.Add(middleRecord.Source())
				}
				offset += rField.Len()
			}
			middleCtx := middleBrick.GetContext(option, middleRecords)
			ctx.Result.MiddleModelPreload[fieldName] = middleCtx.Result
			if err := middleCtx.Next(); err != nil {
				return err
			}
		}
		return nil
	}
}

func HandlerInsertTimeGenerate(ctx *Context) error {
	records := ctx.Result.Records
	createField := ctx.Brick.Model.GetFieldWithName("CreatedAt")
	updateField := ctx.Brick.Model.GetFieldWithName("UpdatedAt")
	if createField != nil || updateField != nil {
		current := time.Now()
		if createField != nil {
			for _, record := range records.GetRecords() {
				record.SetField(createField.Name(), reflect.ValueOf(current))
			}
		}
		if updateField != nil {
			for _, record := range records.GetRecords() {
				record.SetField(updateField.Name(), reflect.ValueOf(current))
			}
		}
	}
	return nil
}

func HandlerInsert(ctx *Context) error {

	// current insert
	setInsertId := len(ctx.Brick.Model.GetPrimary()) == 1 && ctx.Brick.Model.GetOnePrimary().AutoIncrement() == true
	for i, record := range ctx.Result.Records.GetRecords() {
		action := ExecAction{affectData: []int{i}}
		var err error
		if ctx.Brick.template == nil {
			action.Exec = ctx.Brick.InsertExec(record)
		} else {
			tempMap := DefaultTemplateExec(ctx)
			values := ctx.Brick.getFieldValuePairWithRecord(ModeInsert, record)
			tempMap["Columns"] = getColumnExec(columnsValueToColumn(values))
			tempMap["Values"] = getValuesExec(values)
			action.Exec, err = ctx.Brick.Toy.Dialect.TemplateExec(*ctx.Brick.template, tempMap)
			if err != nil {
				return err
			}
		}
		var executor Executor
		if ctx.Brick.tx != nil {
			executor = ctx.Brick.tx
		} else {
			executor = ctx.Brick.Toy.db
		}
		action.Result, action.Error = ctx.Brick.Toy.Dialect.InsertExecutor(
			executor,
			action.Exec,
			func(query string, args string, err error) {
				ctx.Brick.debugPrint(query, args, err)
			},
		)
		if action.Error == nil {
			// set primary field value if model is autoincrement
			if setInsertId {
				primaryKeyName := ctx.Brick.Model.GetOnePrimary().Name()
				// just set not zero primary key
				if fieldValue := record.Field(primaryKeyName); !fieldValue.IsValid() || IsZero(fieldValue) {
					if lastId, err := action.Result.LastInsertId(); err == nil {
						ctx.Result.Records.GetRecord(i).SetField(primaryKeyName, reflect.ValueOf(lastId))
					} else {
						return errors.New(fmt.Sprintf("get (%s) auto increment  failure reason(%s)", ctx.Brick.Model.Name, err))
					}
				}
			}
		}
		ctx.Result.AddRecord(action)
	}
	return nil
}

func HandlerFind(ctx *Context) error {
	var action QueryAction
	var err error
	// use template or use default exec
	if ctx.Brick.template == nil {
		action.Exec = ctx.Brick.FindExec(ctx.Result.Records)
	} else {
		tempMap := DefaultTemplateExec(ctx)
		tempMap["Columns"] = getColumnExec(ctx.Brick.getSelectFields(ctx.Result.Records))
		action.Exec, err = ctx.Brick.Toy.Dialect.TemplateExec(*ctx.Brick.template, tempMap)
		if err != nil {
			return err
		}
	}
	rows, err := ctx.Brick.Query(action.Exec)
	if err != nil {
		action.Error = append(action.Error, err)
		ctx.Result.AddRecord(action)
		return err
	}
	defer rows.Close()
	// find current data
	min := ctx.Result.Records.Len()
	for rows.Next() {
		elem := reflect.New(ctx.Result.Records.ElemType()).Elem()
		ctx.Result.Records.Len()
		record := ctx.Result.Records.Add(elem)

		var scanners []interface{}
		for _, field := range ctx.Brick.getScanFields(ctx.Result.Records) {
			value := record.Field(field.Name())
			scanners = append(scanners, value.Addr().Interface())
		}
		err := rows.Scan(scanners...)
		action.Error = append(action.Error, err)
	}
	max := ctx.Result.Records.Len()
	action.affectData = makeRange(min, max)
	ctx.Result.AddRecord(action)
	return nil
}

func HandlerPreloadContainerCheck(ctx *Context) error {
	for fieldName, preload := range ctx.Brick.BelongToPreload {
		if fieldType := ctx.Result.Records.GetFieldType(fieldName); fieldType == nil {
			return errors.New(fmt.Sprintf("struct missing %s field", fieldName))
		} else {
			subRecords := MakeRecordsWithElem(preload.SubModel, fieldType)
			subPrimaryFieldName := preload.SubModel.GetOnePrimary().Name()
			if relationFieldType := subRecords.GetFieldType(subPrimaryFieldName); relationFieldType == nil {
				return errors.New(fmt.Sprintf("struct of the %s field missing %s field", fieldName, subPrimaryFieldName))
			}
		}
		if fieldType := ctx.Result.Records.GetFieldType(preload.RelationField.Name()); fieldType == nil {
			return errors.New(fmt.Sprintf("struct missing %s field", preload.RelationField.Name()))
		}
	}
	var needPrimaryKey bool
	for fieldName, preload := range ctx.Brick.OneToOnePreload {
		needPrimaryKey = true
		if fieldType := ctx.Result.Records.GetFieldType(fieldName); fieldType == nil {
			return errors.New(fmt.Sprintf("struct missing %s field", fieldName))
		} else {
			subRecords := MakeRecordsWithElem(preload.SubModel, fieldType)
			if relationFieldType := subRecords.GetFieldType(preload.RelationField.Name()); relationFieldType == nil {
				return errors.New(fmt.Sprintf("struct of the %s field missing %s field", fieldName, preload.RelationField.Name()))
			}
		}
	}
	for fieldName, preload := range ctx.Brick.OneToManyPreload {
		needPrimaryKey = true
		if fieldType := ctx.Result.Records.GetFieldType(fieldName); fieldType == nil {
			return errors.New(fmt.Sprintf("struct missing %s field", fieldName))
		} else {
			subRecords := MakeRecordsWithElem(preload.SubModel, fieldType)
			if relationFieldType := subRecords.GetFieldType(preload.RelationField.Name()); relationFieldType == nil {
				return errors.New(fmt.Sprintf("struct of the %s field missing %s field", fieldName, preload.RelationField.Name()))
			}
		}
	}
	for fieldName, preload := range ctx.Brick.ManyToManyPreload {
		needPrimaryKey = true
		if fieldType := ctx.Result.Records.GetFieldType(fieldName); fieldType == nil {
			return errors.New(fmt.Sprintf("struct missing %s field", fieldName))
		} else {
			subRecords := MakeRecordsWithElem(preload.SubModel, fieldType)
			subPrimaryFieldName := preload.SubModel.GetOnePrimary().Name()
			if relationFieldType := subRecords.GetFieldType(subPrimaryFieldName); relationFieldType == nil {
				return errors.New(fmt.Sprintf("struct of the %s field missing %s field", fieldName, subPrimaryFieldName))
			}
		}
	}
	if needPrimaryKey {
		primaryName := ctx.Brick.Model.GetOnePrimary().Name()
		if primaryType := ctx.Result.Records.GetFieldType(primaryName); primaryType == nil {
			return errors.New(fmt.Sprintf("struct missing %s field", primaryName))
		}
	}
	return nil
}

func HandlerPreloadFind(ctx *Context) error {
	for fieldName, preload := range ctx.Brick.BelongToPreload {
		mainField, subField := preload.RelationField, preload.SubModel.GetOnePrimary()
		brick := ctx.Brick.MapPreloadBrick[fieldName]

		mainGroup := ctx.Result.Records.GroupBy(mainField.Name())

		delete(mainGroup, reflect.Zero(mainField.StructField().Type))
		if keys := mainGroup.Keys(); len(keys) != 0 {
			// the relation condition should have lowest priority
			brick = brick.Where(ExprIn, subField, keys).And().Conditions(brick.Search)
			containerList := reflect.New(reflect.SliceOf(ctx.Result.Records.GetFieldType(fieldName))).Elem()
			//var preloadRecords ModelRecords
			subCtx, err := brick.find(LoopIndirectAndNew(containerList))
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err != nil {
				return err
			}
			// set sub data to container field
			subGroup := subCtx.Result.Records.GroupBy(subField.Name())
			ctx.Result.SimpleRelation[fieldName] = map[int]int{}
			for key, records := range mainGroup {
				if subRecords := subGroup[key]; len(subRecords) != 0 {
					for _, record := range records {
						record.SetField(preload.ContainerField.Name(), subRecords[0].Source())
						ctx.Result.SimpleRelation[fieldName][subRecords[0].Index] = record.Index
					}
				}
			}
		}
	}
	for fieldName, preload := range ctx.Brick.OneToOnePreload {
		var mainField, subField Field
		mainField, subField = preload.Model.GetOnePrimary(), preload.RelationField
		brick := ctx.Brick.MapPreloadBrick[fieldName]

		mainGroup := ctx.Result.Records.GroupBy(mainField.Name())
		delete(mainGroup, reflect.Zero(mainField.StructField().Type))
		if keys := mainGroup.Keys(); len(keys) != 0 {
			// the relation condition should have lowest priority
			brick = brick.Where(ExprIn, subField, keys).And().Conditions(brick.Search)
			containerList := reflect.New(reflect.SliceOf(ctx.Result.Records.GetFieldType(fieldName))).Elem()
			//var preloadRecords ModelRecords
			subCtx, err := brick.find(LoopIndirectAndNew(containerList))
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err != nil {
				return err
			}
			// set sub data to container field
			ctx.Result.SimpleRelation[fieldName] = map[int]int{}
			subGroup := subCtx.Result.Records.GroupBy(subField.Name())
			for key, records := range mainGroup {
				if subRecords := subGroup[key]; len(subRecords) != 0 {
					for _, record := range records {
						record.SetField(preload.ContainerField.Name(), subRecords[0].Source())
						ctx.Result.SimpleRelation[fieldName][subRecords[0].Index] = record.Index
					}
				}
			}
		}
	}
	// one to many
	for fieldName, preload := range ctx.Brick.OneToManyPreload {
		mainField, subField := preload.Model.GetOnePrimary(), preload.RelationField
		brick := ctx.Brick.MapPreloadBrick[fieldName]

		mainGroup := ctx.Result.Records.GroupBy(mainField.Name())
		delete(mainGroup, reflect.Zero(mainField.StructField().Type))
		if keys := mainGroup.Keys(); len(keys) != 0 {
			// the relation condition should have lowest priority
			brick = brick.Where(ExprIn, subField, keys).And().Conditions(brick.Search)
			containerList := reflect.New(ctx.Result.Records.GetFieldType(fieldName)).Elem()

			subCtx, err := brick.find(LoopIndirectAndNew(containerList))
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err != nil {
				return err
			}
			subGroup := subCtx.Result.Records.GroupBy(subField.Name())

			ctx.Result.MultipleRelation[fieldName] = map[int]Pair{}
			for key, records := range mainGroup {
				if subRecords := subGroup[key]; len(subRecords) != 0 {
					for _, record := range records {
						container := record.Field(preload.ContainerField.Name())
						containerIndirect := LoopIndirectAndNew(container)
						for j, subRecord := range subRecords {
							containerIndirect.Set(SafeAppend(containerIndirect, subRecord.Source()))
							ctx.Result.MultipleRelation[fieldName][subRecord.Index] = Pair{record.Index, j}
						}
					}
				}
			}
		}
	}
	// many to many
	for fieldName, preload := range ctx.Brick.ManyToManyPreload {
		mainPrimary, subPrimary := preload.Model.GetOnePrimary(), preload.SubModel.GetOnePrimary()
		middleBrick := NewToyBrick(ctx.Brick.Toy, preload.MiddleModel).CopyStatus(ctx.Brick)

		// primaryMap: map[model.id]->the model's ModelRecord
		//primaryMap := map[interface{}]ModelRecord{}
		mainGroup := ctx.Result.Records.GroupBy(mainPrimary.Name())
		if keys := mainGroup.Keys(); len(keys) != 0 {
			// the relation condition should have lowest priority
			middleBrick = middleBrick.Where(ExprIn, preload.RelationField, keys).And().Conditions(middleBrick.Search)
			middleModelElemList := reflect.New(reflect.SliceOf(preload.MiddleModel.ReflectType)).Elem()
			//var middleModelRecords ModelRecords
			middleCtx, err := middleBrick.find(middleModelElemList)
			ctx.Result.MiddleModelPreload[fieldName] = middleCtx.Result
			if err != nil {
				return err
			}
			middleGroup := middleCtx.Result.Records.GroupBy(preload.SubRelationField.Name())
			if subKeys := middleGroup.Keys(); len(subKeys) != 0 {
				brick := ctx.Brick.MapPreloadBrick[fieldName]
				// the relation condition should have lowest priority
				brick = brick.Where(ExprIn, subPrimary, subKeys).And().Conditions(brick.Search)
				containerField := reflect.New(ctx.Result.Records.GetFieldType(fieldName)).Elem()
				//var subRecords ModelRecords
				subCtx, err := brick.find(LoopIndirectAndNew(containerField))
				ctx.Result.Preload[fieldName] = subCtx.Result
				if err != nil {
					return err
				}

				ctx.Result.MultipleRelation[fieldName] = map[int]Pair{}
				for j, subRecord := range subCtx.Result.Records.GetRecords() {
					if middleRecords := middleGroup[subRecord.Field(subPrimary.Name()).Interface()]; len(middleRecords) != 0 {
						for _, middleRecord := range middleRecords {
							mainRecord := mainGroup[middleRecord.Field(preload.RelationField.Name()).Interface()][0]
							name := preload.ContainerField.Name()
							container := mainRecord.Field(name)
							containerIndirect := LoopIndirectAndNew(container)
							subi := containerIndirect.Len()
							containerIndirect.Set(SafeAppend(containerIndirect, subRecord.Source()))
							ctx.Result.MultipleRelation[fieldName][j] = Pair{mainRecord.Index, subi}
						}
					}
				}

			}

		}
	}
	return nil
}

func HandlerUpdateTimeGenerate(ctx *Context) error {
	records := ctx.Result.Records
	if updateField := ctx.Brick.Model.GetFieldWithName("UpdatedAt"); updateField != nil {
		current := reflect.ValueOf(time.Now())
		for _, record := range records.GetRecords() {
			record.SetField(updateField.Name(), current)
		}
	}
	return nil
}

func HandlerUpdate(ctx *Context) error {
	for i, record := range ctx.Result.Records.GetRecords() {
		action := ExecAction{affectData: []int{i}}
		var err error
		if ctx.Brick.template == nil {
			action.Exec = ctx.Brick.UpdateExec(record)
		} else {
			tempMap := DefaultTemplateExec(ctx)
			values := ctx.Brick.getFieldValuePairWithRecord(ModeUpdate, record)
			tempMap["Columns"] = getColumnExec(columnsValueToColumn(values))
			tempMap["Values"] = getUpdateValuesExec(values)
			action.Exec, err = ctx.Brick.Toy.Dialect.TemplateExec(*ctx.Brick.template, tempMap)
			if err != nil {
				return err
			}
		}

		action.Result, action.Error = ctx.Brick.Exec(action.Exec)
		ctx.Result.AddRecord(action)
	}
	return nil
}

// if have not primary ,try to insert
// else try to replace
func HandlerSave(ctx *Context) error {
	setInsertId := len(ctx.Brick.Model.GetPrimary()) == 1 && ctx.Brick.Model.GetOnePrimary().AutoIncrement() == true
	for i, record := range ctx.Result.Records.GetRecords() {
		primaryFields := ctx.Brick.Model.GetPrimary()
		var tryInsert bool
		for _, primaryField := range primaryFields {
			pkeyFieldValue := record.Field(primaryField.Name())
			if pkeyFieldValue.IsValid() == false || IsZero(pkeyFieldValue) {
				tryInsert = true
				break
			}
		}
		var action ExecAction
		var err error
		if tryInsert {
			action = ExecAction{affectData: []int{i}}
			if ctx.Brick.template == nil {
				action.Exec = ctx.Brick.InsertExec(record)
			} else {
				tempMap := DefaultTemplateExec(ctx)
				values := ctx.Brick.getFieldValuePairWithRecord(ModeInsert, record)
				tempMap["Columns"] = getColumnExec(columnsValueToColumn(values))
				tempMap["Values"] = getValuesExec(values)
				tempMap["UpdateValues"] = getUpdateValuesExec(values)
				action.Exec, err = ctx.Brick.Toy.Dialect.TemplateExec(*ctx.Brick.template, tempMap)
				if err != nil {
					return err
				}
			}
			var executor Executor
			if ctx.Brick.tx != nil {
				executor = ctx.Brick.tx
			} else {
				executor = ctx.Brick.Toy.db
			}
			action.Result, action.Error = ctx.Brick.Toy.Dialect.InsertExecutor(
				executor,
				action.Exec,
				func(query string, args string, err error) {
					ctx.Brick.debugPrint(query, args, err)
				},
			)
			if action.Error == nil {
				// set primary field value if model is autoincrement
				if setInsertId {
					primaryKeyName := ctx.Brick.Model.GetOnePrimary().Name()
					// just set not zero primary key
					if fieldValue := record.Field(primaryKeyName); !fieldValue.IsValid() || IsZero(fieldValue) {
						if lastId, err := action.Result.LastInsertId(); err == nil {
							ctx.Result.Records.GetRecord(i).SetField(primaryKeyName, reflect.ValueOf(lastId))
						} else {
							return errors.New(fmt.Sprintf("get (%s) auto increment  failure reason(%s)", ctx.Brick.Model.Name, err))
						}
					}
				}
			}
		} else {
			action = ExecAction{affectData: []int{i}}
			if ctx.Brick.template == nil {
				action.Exec = ctx.Brick.ReplaceExec(record)
			} else {
				tempMap := DefaultTemplateExec(ctx)
				values := ctx.Brick.getFieldValuePairWithRecord(ModeReplace, record)
				tempMap["Columns"] = getColumnExec(columnsValueToColumn(values))
				tempMap["Values"] = getValuesExec(values)
				tempMap["UpdateValues"] = getUpdateValuesExec(values)
				action.Exec, err = ctx.Brick.Toy.Dialect.TemplateExec(*ctx.Brick.template, tempMap)
				if err != nil {
					return err
				}
			}
			action.Result, action.Error = ctx.Brick.Exec(action.Exec)
		}
		ctx.Result.AddRecord(action)
	}
	return nil
}

func HandlerSaveTimeGenerate(ctx *Context) error {
	createdAtField := ctx.Brick.Model.GetFieldWithName("CreatedAt")
	deletedAtField := ctx.Brick.Model.GetFieldWithName("DeletedAt")
	now := reflect.ValueOf(time.Now())

	var timeFields []Field
	var defaultFieldValue []reflect.Value
	if createdAtField != nil {
		timeFields = append(timeFields, createdAtField)
		defaultFieldValue = append(defaultFieldValue, now)
	}
	if deletedAtField != nil {
		timeFields = append(timeFields, deletedAtField)
		defaultFieldValue = append(defaultFieldValue, reflect.Zero(deletedAtField.StructField().Type))
	}

	if ctx.Result.Records.Len() > 0 && len(timeFields) > 0 {
		primaryField := ctx.Brick.Model.GetOnePrimary()
		brick := ctx.Brick.bindFields(ModeDefault, append([]Field{primaryField}, timeFields...)...)
		primaryKeys := reflect.MakeSlice(reflect.SliceOf(primaryField.StructField().Type), 0, ctx.Result.Records.Len())
		action := QueryAction{}

		for i, record := range ctx.Result.Records.GetRecords() {
			pri := record.Field(primaryField.Name())
			if pri.IsValid() && IsZero(pri) == false {
				primaryKeys = reflect.Append(primaryKeys, pri)
				action.affectData = append(action.affectData, i)
			}
		}

		if primaryKeys.Len() > 0 {
			action.Exec = brick.Where(ExprIn, primaryField, primaryKeys.Interface()).FindExec(ctx.Result.Records)

			rows, err := brick.Query(action.Exec)
			if err != nil {
				action.Error = append(action.Error, err)

				ctx.Result.AddRecord(action)
				return nil
			}
			defer rows.Close()
			var mapElemTypeFields []reflect.StructField
			{
				for _, f := range timeFields {
					mapElemTypeFields = append(mapElemTypeFields, f.StructField())
				}
			}
			mapElemType := reflect.StructOf(mapElemTypeFields)
			primaryKeysMap := reflect.MakeMap(reflect.MapOf(primaryField.StructField().Type, mapElemType))

			// find all createtime
			for rows.Next() {
				id := reflect.New(primaryField.StructField().Type)
				timeFieldValues := reflect.New(mapElemType).Elem()
				scaners := []interface{}{id.Interface()}
				for i := 0; i < timeFieldValues.NumField(); i++ {
					scaners = append(scaners, timeFieldValues.Field(i).Addr().Interface())
				}
				err := rows.Scan(scaners...)
				if err != nil {
					action.Error = append(action.Error, err)
				}
				primaryKeysMap.SetMapIndex(id.Elem(), timeFieldValues)
			}

			ctx.Result.AddRecord(action)
			for _, record := range ctx.Result.Records.GetRecords() {
				pri := record.Field(primaryField.Name())
				fields := primaryKeysMap.MapIndex(pri)
				if fields.IsValid() {
					for i := 0; i < fields.NumField(); i++ {
						field := fields.Field(i)
						if field.IsValid() && IsZero(field) == false {
							record.SetField(timeFields[i].Name(), field)
						} else if IsZero(record.Field(timeFields[i].Name())) {
							record.SetField(timeFields[i].Name(), defaultFieldValue[i])
						}
					}
				} else {
					for i := 0; i < len(timeFields); i++ {
						if IsZero(record.Field(timeFields[i].Name())) {
							record.SetField(timeFields[i].Name(), defaultFieldValue[i])
						}
					}
				}
			}
		} else {
			for _, record := range ctx.Result.Records.GetRecords() {
				for i := 0; i < len(timeFields); i++ {
					if IsZero(record.Field(timeFields[i].Name())) {
						record.SetField(timeFields[i].Name(), defaultFieldValue[i])
					}
				}
			}
		}
	}
	if updateField := ctx.Brick.Model.GetFieldWithName("UpdatedAt"); updateField != nil {
		for _, record := range ctx.Result.Records.GetRecords() {
			record.SetField(updateField.Name(), now)
		}
	}
	return nil
}

// preload schedule belongTo -> Next() -> oneToOne -> oneToMany -> manyToMany(sub -> middle)
func HandlerCreateTablePreload(option string) func(ctx *Context) error {
	return func(ctx *Context) (err error) {
		for fieldName := range ctx.Brick.BelongToPreload {
			brick := ctx.Brick.MapPreloadBrick[fieldName]
			subCtx := brick.GetContext(option, MakeRecordsWithElem(brick.Model, brick.Model.ReflectType))
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err := subCtx.Next(); err != nil {
				return err
			}
		}
		err = ctx.Next()
		if err != nil {
			return err
		}
		for fieldName := range ctx.Brick.OneToOnePreload {
			brick := ctx.Brick.MapPreloadBrick[fieldName]
			subCtx := brick.GetContext(option, MakeRecordsWithElem(brick.Model, brick.Model.ReflectType))
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err := subCtx.Next(); err != nil {
				return err
			}
		}

		for fieldName := range ctx.Brick.OneToManyPreload {
			brick := ctx.Brick.MapPreloadBrick[fieldName]
			subCtx := brick.GetContext(option, MakeRecordsWithElem(brick.Model, brick.Model.ReflectType))
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err := subCtx.Next(); err != nil {
				return err
			}
		}
		for fieldName, preload := range ctx.Brick.ManyToManyPreload {
			{
				brick := ctx.Brick.MapPreloadBrick[fieldName]
				subCtx := brick.GetContext(option, MakeRecordsWithElem(brick.Model, brick.Model.ReflectType))
				ctx.Result.Preload[fieldName] = subCtx.Result
				if err := subCtx.Next(); err != nil {
					return err
				}
			}
			// process middle model
			{
				middleModel := preload.MiddleModel
				brick := NewToyBrick(ctx.Brick.Toy, middleModel).CopyStatus(ctx.Brick)
				// copy PreToyBrick
				brick = brick.Scope(func(t *ToyBrick) *ToyBrick {
					newt := *t
					newt.preBrick = PreToyBrick{
						ctx.Brick,
						preload.ContainerField,
					}
					return &newt
				})
				middleCtx := brick.GetContext(option, MakeRecordsWithElem(brick.Model, brick.Model.ReflectType))
				ctx.Result.MiddleModelPreload[fieldName] = middleCtx.Result
				if err := middleCtx.Next(); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

// preload schedule oneToOne -> oneToMany -> current model -> manyToMany(sub -> middle) -> Next() -> belongTo
func HandlerDropTablePreload(option string) func(ctx *Context) error {
	return func(ctx *Context) (err error) {
		for fieldName := range ctx.Brick.OneToOnePreload {
			brick := ctx.Brick.MapPreloadBrick[fieldName]
			subCtx := brick.GetContext(option, MakeRecordsWithElem(brick.Model, brick.Model.ReflectType))
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err := subCtx.Next(); err != nil {
				return err
			}

		}
		for fieldName := range ctx.Brick.OneToManyPreload {
			brick := ctx.Brick.MapPreloadBrick[fieldName]
			subCtx := brick.GetContext(option, MakeRecordsWithElem(brick.Model, brick.Model.ReflectType))
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err := subCtx.Next(); err != nil {
				return err
			}
		}
		for fieldName, preload := range ctx.Brick.ManyToManyPreload {
			// process middle model
			{
				middleModel := preload.MiddleModel
				brick := NewToyBrick(ctx.Brick.Toy, middleModel).CopyStatus(ctx.Brick)
				middleCtx := brick.GetContext(option, MakeRecordsWithElem(brick.Model, brick.Model.ReflectType))
				ctx.Result.MiddleModelPreload[fieldName] = middleCtx.Result
				if err := middleCtx.Next(); err != nil {
					return err
				}
			}
			// process sub model
			{
				brick := ctx.Brick.MapPreloadBrick[fieldName]
				subCtx := brick.GetContext(option, MakeRecordsWithElem(brick.Model, brick.Model.ReflectType))
				ctx.Result.Preload[fieldName] = subCtx.Result
				if err := subCtx.Next(); err != nil {
					return err
				}
			}
		}
		err = ctx.Next()
		if err != nil {
			return err
		}
		for fieldName := range ctx.Brick.BelongToPreload {
			brick := ctx.Brick.MapPreloadBrick[fieldName]
			subCtx := brick.GetContext(option, MakeRecordsWithElem(brick.Model, brick.Model.ReflectType))
			ctx.Result.Preload[fieldName] = subCtx.Result
			if err := subCtx.Next(); err != nil {
				return err
			}

		}

		return nil
	}
}

func HandlerCreateTable(ctx *Context) error {
	foreign := map[string]ForeignKey{}
	for _, field := range ctx.Brick.Model.GetSqlFields() {
		// this is foreign key, mean it must relationship field with parent or child
		if field.IsForeign() {
			if ctx.Brick.preBrick.Parent != nil {
				parent, containerField := ctx.Brick.preBrick.Parent, ctx.Brick.preBrick.Field
				if preload := parent.OneToOnePreload[containerField.Name()]; preload != nil {
					if preload.RelationField.Name() == field.Name() {
						foreign[field.Name()] = ForeignKey{preload.Model, preload.Model.GetOnePrimary()}
					}
				} else if preload := parent.OneToManyPreload[containerField.Name()]; preload != nil {
					if preload.RelationField.Name() == field.Name() {
						foreign[field.Name()] = ForeignKey{preload.Model, preload.Model.GetOnePrimary()}
					}
				} else if preload := parent.ManyToManyPreload[containerField.Name()]; preload != nil {
					if preload.SubRelationField.Name() == field.Name() {
						foreign[field.Name()] = ForeignKey{preload.SubModel, preload.SubModel.GetOnePrimary()}
					} else if preload.RelationField.Name() == field.Name() {
						foreign[field.Name()] = ForeignKey{preload.Model, preload.Model.GetOnePrimary()}
					}
				}
			}
			// search belong to
			for _, preload := range ctx.Brick.BelongToPreload {
				if preload.RelationField.Name() == field.Name() {
					foreign[field.Name()] = ForeignKey{preload.SubModel, preload.SubModel.GetOnePrimary()}
				}
			}
		}
	}

	execs := ctx.Brick.Toy.Dialect.CreateTable(ctx.Brick.Model, foreign)
	for _, exec := range execs {
		action := ExecAction{Exec: exec}
		action.Result, action.Error = ctx.Brick.Exec(exec)
		ctx.Result.AddRecord(action)
	}
	return nil
}

func HandlerExistTableAbort(ctx *Context) error {
	action := QueryAction{}
	action.Exec = ctx.Brick.Toy.Dialect.HasTable(ctx.Brick.Model)
	var hasTable bool
	err := ctx.Brick.QueryRow(action.Exec).Scan(&hasTable)
	if err != nil {
		action.Error = append(action.Error, err)
	}
	ctx.Result.AddRecord(action)
	if err != nil || hasTable == true {
		ctx.Abort()
	}

	return nil
}

func HandlerDropTable(ctx *Context) (err error) {
	exec := ctx.Brick.Toy.Dialect.DropTable(ctx.Brick.Model)
	action := ExecAction{Exec: exec}
	action.Result, action.Error = ctx.Brick.Exec(exec)
	ctx.Result.AddRecord(action)
	return nil
}

func HandlerNotExistTableAbort(ctx *Context) error {
	action := QueryAction{}
	action.Exec = ctx.Brick.Toy.Dialect.HasTable(ctx.Brick.Model)
	var hasTable bool
	err := ctx.Brick.QueryRow(action.Exec).Scan(&hasTable)
	if err != nil {
		action.Error = append(action.Error, err)
	}
	ctx.Result.AddRecord(action)
	if err != nil || hasTable == false {
		ctx.Abort()
	}
	return nil
}

func HandlerPreloadDelete(ctx *Context) error {
	for fieldName, preload := range ctx.Brick.OneToOnePreload {
		preloadBrick := ctx.Brick.MapPreloadBrick[fieldName]
		subRecords := MakeRecordsWithElem(preload.SubModel, ctx.Result.Records.GetFieldAddressType(fieldName))
		mainSoftDelete := preload.Model.GetFieldWithName("DeletedAt") != nil
		subSoftDelete := preload.SubModel.GetFieldWithName("DeletedAt") != nil
		// set sub model relation field
		for _, record := range ctx.Result.Records.GetRecords() {
			// it means relation field, result[j].LastInsertId() is id value
			subRecords.Add(record.FieldAddress(fieldName))
		}
		// if main model is hard delete need set relationship field set zero if sub model is soft delete
		if mainSoftDelete == false && subSoftDelete == true {
			deletedAtField := preloadBrick.Model.GetFieldWithName("DeletedAt")
			preloadBrick = preloadBrick.bindDefaultFields(preload.RelationField, deletedAtField)
		}
		result, err := preloadBrick.deleteWithPrimaryKey(subRecords)
		ctx.Result.Preload[fieldName] = result
		if err != nil {
			return err
		}
	}

	// one to many
	for fieldName, preload := range ctx.Brick.OneToManyPreload {
		preloadBrick := ctx.Brick.MapPreloadBrick[fieldName]
		mainSoftDelete := preload.Model.GetFieldWithName("DeletedAt") != nil
		subSoftDelete := preload.SubModel.GetFieldWithName("DeletedAt") != nil
		elemAddressType := reflect.PtrTo(LoopTypeIndirect(ctx.Result.Records.GetFieldType(fieldName)).Elem())
		subRecords := MakeRecordsWithElem(preload.SubModel, elemAddressType)
		for _, record := range ctx.Result.Records.GetRecords() {
			rField := LoopIndirect(record.Field(fieldName))
			for subi := 0; subi < rField.Len(); subi++ {
				subRecords.Add(rField.Index(subi).Addr())
			}
		}
		// model relationship field set zero
		if mainSoftDelete == false && subSoftDelete == true {
			deletedAtField := preloadBrick.Model.GetFieldWithName("DeletedAt")
			preloadBrick = preloadBrick.bindDefaultFields(preload.RelationField, deletedAtField)
		}
		result, err := preloadBrick.deleteWithPrimaryKey(subRecords)
		ctx.Result.Preload[fieldName] = result
		if err != nil {
			return err
		}
	}
	// many to many
	for fieldName, preload := range ctx.Brick.ManyToManyPreload {
		subBrick := ctx.Brick.MapPreloadBrick[fieldName]
		middleBrick := NewToyBrick(ctx.Brick.Toy, preload.MiddleModel).CopyStatus(ctx.Brick)
		mainField, subField := preload.Model.GetOnePrimary(), preload.SubModel.GetOnePrimary()
		mainSoftDelete := preload.Model.GetFieldWithName("DeletedAt") != nil
		subSoftDelete := preload.SubModel.GetFieldWithName("DeletedAt") != nil

		elemAddressType := reflect.PtrTo(LoopTypeIndirect(ctx.Result.Records.GetFieldType(fieldName)).Elem())
		subRecords := MakeRecordsWithElem(preload.SubModel, elemAddressType)

		for _, record := range ctx.Result.Records.GetRecords() {
			rField := LoopIndirect(record.Field(fieldName))
			for subi := 0; subi < rField.Len(); subi++ {
				subRecords.Add(rField.Index(subi).Addr())
			}
		}

		middleRecords := MakeRecordsWithElem(middleBrick.Model, middleBrick.Model.ReflectType)
		// use to calculate what sub records belong for
		offset := 0
		for _, record := range ctx.Result.Records.GetRecords() {
			primary := record.Field(mainField.Name())
			if primary.IsValid() == false {
				return errors.New("some records have not primary key")
			}
			rField := LoopIndirect(record.Field(fieldName))
			for subi := 0; subi < rField.Len(); subi++ {
				subRecord := subRecords.GetRecord(subi + offset)
				subPrimary := subRecord.Field(subField.Name())
				if subPrimary.IsValid() == false {
					return errors.New("some records have not primary key")
				}
				middleRecord := NewRecord(middleBrick.Model, reflect.New(middleBrick.Model.ReflectType).Elem())
				middleRecord.SetField(preload.RelationField.Name(), primary)
				middleRecord.SetField(preload.SubRelationField.Name(), subPrimary)
				middleRecords.Add(middleRecord.Source())
			}
			offset += rField.Len()
		}

		// delete middle model data
		var primaryFields []Field
		if mainSoftDelete == false {
			primaryFields = append(primaryFields, middleBrick.Model.GetPrimary()[0])
		}
		if subSoftDelete == false {
			primaryFields = append(primaryFields, middleBrick.Model.GetPrimary()[1])
		}
		if len(primaryFields) != 0 {
			conditions := middleBrick.Search
			middleBrick = middleBrick.Conditions(nil)
			for _, primaryField := range primaryFields {
				primarySetType := reflect.MapOf(primaryField.StructField().Type, reflect.TypeOf(struct{}{}))
				primarySet := reflect.MakeMap(primarySetType)
				for _, record := range middleRecords.GetRecords() {
					primarySet.SetMapIndex(record.Field(primaryField.Name()), reflect.ValueOf(struct{}{}))
				}
				var primaryKeys = reflect.New(reflect.SliceOf(primaryField.StructField().Type)).Elem()
				for _, k := range primarySet.MapKeys() {
					primaryKeys = reflect.Append(primaryKeys, k)
				}
				middleBrick = middleBrick.Where(ExprIn, primaryField, primaryKeys.Interface()).
					Or().Conditions(middleBrick.Search)
			}
			middleBrick = middleBrick.And().Conditions(conditions)
			result, err := middleBrick.delete(middleRecords)
			ctx.Result.MiddleModelPreload[fieldName] = result
			if err != nil {
				return err
			}
		}

		result, err := subBrick.deleteWithPrimaryKey(subRecords)
		ctx.Result.Preload[fieldName] = result
		if err != nil {
			return err
		}
	}

	if err := ctx.Next(); err != nil {
		return err
	}

	for fieldName, preload := range ctx.Brick.BelongToPreload {
		preloadBrick := ctx.Brick.MapPreloadBrick[fieldName]
		subRecords := MakeRecordsWithElem(preload.SubModel, ctx.Result.Records.GetFieldAddressType(fieldName))
		for _, record := range ctx.Result.Records.GetRecords() {
			subRecords.Add(record.FieldAddress(fieldName))
		}

		mainSoftDelete := preload.Model.GetFieldWithName("DeletedAt") != nil
		subSoftDelete := preload.SubModel.GetFieldWithName("DeletedAt") != nil
		if mainSoftDelete == false && subSoftDelete == true {
			deletedAtField := preloadBrick.Model.GetFieldWithName("DeletedAt")
			preloadBrick = preloadBrick.bindDefaultFields(preload.RelationField, deletedAtField)
		}

		result, err := preloadBrick.deleteWithPrimaryKey(subRecords)
		ctx.Result.Preload[fieldName] = result
		if err != nil {
			return err
		}

	}
	return nil
}

func HandlerSearchWithPrimaryKey(ctx *Context) error {
	var primaryKeys []interface{}
	primaryField := ctx.Brick.Model.GetOnePrimary()
	for _, record := range ctx.Result.Records.GetRecords() {
		primaryKeys = append(primaryKeys, record.Field(primaryField.Name()).Interface())
	}
	if len(primaryKeys) == 0 {
		ctx.Abort()
		return nil
	}
	ctx.Brick = ctx.Brick.Where(ExprIn, primaryField, primaryKeys).And().Conditions(ctx.Brick.Search)
	return nil
}

func HandlerHardDelete(ctx *Context) error {
	action := ExecAction{}
	action.Exec = ctx.Brick.DeleteExec()
	action.Result, action.Error = ctx.Brick.Exec(action.Exec)
	ctx.Result.AddRecord(action)
	return nil
}

//
func HandlerSoftDeleteCheck(ctx *Context) error {
	deletedField := ctx.Brick.Model.GetFieldWithName("DeletedAt")
	if deletedField != nil {
		ctx.Brick = ctx.Brick.Where(ExprNull, deletedField).And().Conditions(ctx.Brick.Search)
	}
	return nil
}

func HandlerSoftDelete(ctx *Context) error {
	action := ExecAction{}
	now := time.Now()
	value := reflect.New(ctx.Brick.Model.ReflectType).Elem()
	record := NewStructRecord(ctx.Brick.Model, value)
	record.SetField("DeletedAt", reflect.ValueOf(now))
	bindFields := []interface{}{"DeletedAt"}
	for _, preload := range ctx.Brick.BelongToPreload {
		subSoftDelete := preload.SubModel.GetFieldWithName("DeletedAt") != nil
		if subSoftDelete == false {
			rField := preload.RelationField
			bindFields = append(bindFields, rField.Name())
			record.SetField(rField.Name(), reflect.Zero(rField.StructField().Type))
		}
	}
	ctx.Brick = ctx.Brick.BindFields(ModeUpdate, bindFields...)
	action.Exec = ctx.Brick.UpdateExec(record)
	action.Result, action.Error = ctx.Brick.Exec(action.Exec)
	ctx.Result.AddRecord(action)
	return nil
}
