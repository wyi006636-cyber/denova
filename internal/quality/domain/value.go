package domain

import (
	"encoding/json"
	"math"
	"math/big"
	"reflect"
	"strconv"
)

func validateGoalValue(goal QualityGoal, value any, path string, layer Layer) error {
	if !valueHasType(value, goal.ValueContract.Type) {
		return &ContractError{
			Code:    CodeInvalidValueType,
			Path:    path,
			GoalID:  goal.ID,
			Layer:   layer,
			Value:   value,
			Message: "value does not match the goal value contract",
		}
	}
	for _, allowed := range goal.ValueContract.AllowedValues {
		if contractValuesEqual(value, allowed) {
			return nil
		}
	}
	return &ContractError{
		Code:    CodeUnsupportedValue,
		Path:    path,
		GoalID:  goal.ID,
		Layer:   layer,
		Value:   value,
		Message: "value is not in the goal allowed_values set",
	}
}

func validateValueContract(goal QualityGoal, path string) error {
	if goal.ValueContract.UnknownValuePolicy != RejectExplicitly || len(goal.ValueContract.AllowedValues) == 0 {
		return &ContractError{
			Code:    CodeUnsupportedValue,
			Path:    path,
			GoalID:  goal.ID,
			Value:   goal.ValueContract.UnknownValuePolicy,
			Message: "value contract must list allowed values and reject unknown values explicitly",
		}
	}
	for index, allowed := range goal.ValueContract.AllowedValues {
		if !valueHasType(allowed, goal.ValueContract.Type) {
			return &ContractError{
				Code:    CodeInvalidValueType,
				Path:    path + ".allowed_values[" + strconv.Itoa(index) + "]",
				GoalID:  goal.ID,
				Value:   allowed,
				Message: "allowed value does not match the declared value type",
			}
		}
	}
	return nil
}

func valueHasType(value any, valueType ValueType) bool {
	switch valueType {
	case ValueTypeBoolean:
		_, ok := value.(bool)
		return ok
	case ValueTypeInteger:
		return isInteger(value)
	case ValueTypeNumber:
		_, ok := numericValue(value)
		return ok
	case ValueTypeString:
		_, ok := value.(string)
		return ok
	case ValueTypeEnum:
		switch value.(type) {
		case string, bool, json.Number,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func isInteger(value any) bool {
	switch number := value.(type) {
	case json.Number:
		rational, ok := numericValue(number)
		return ok && rational.IsInt()
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return !math.IsInf(float64(number), 0) && !math.IsNaN(float64(number)) && number == float32(math.Trunc(float64(number)))
	case float64:
		return !math.IsInf(number, 0) && !math.IsNaN(number) && number == math.Trunc(number)
	default:
		return false
	}
}

func contractValuesEqual(left, right any) bool {
	leftNumber, leftIsNumber := numericValue(left)
	rightNumber, rightIsNumber := numericValue(right)
	if leftIsNumber || rightIsNumber {
		return leftIsNumber && rightIsNumber && leftNumber.Cmp(rightNumber) == 0
	}
	return reflect.DeepEqual(left, right)
}

func numericValue(value any) (*big.Rat, bool) {
	var text string
	switch number := value.(type) {
	case json.Number:
		text = number.String()
	case int:
		text = strconv.FormatInt(int64(number), 10)
	case int8:
		text = strconv.FormatInt(int64(number), 10)
	case int16:
		text = strconv.FormatInt(int64(number), 10)
	case int32:
		text = strconv.FormatInt(int64(number), 10)
	case int64:
		text = strconv.FormatInt(number, 10)
	case uint:
		text = strconv.FormatUint(uint64(number), 10)
	case uint8:
		text = strconv.FormatUint(uint64(number), 10)
	case uint16:
		text = strconv.FormatUint(uint64(number), 10)
	case uint32:
		text = strconv.FormatUint(uint64(number), 10)
	case uint64:
		text = strconv.FormatUint(number, 10)
	case float32:
		if math.IsInf(float64(number), 0) || math.IsNaN(float64(number)) {
			return nil, false
		}
		text = strconv.FormatFloat(float64(number), 'g', -1, 32)
	case float64:
		if math.IsInf(number, 0) || math.IsNaN(number) {
			return nil, false
		}
		text = strconv.FormatFloat(number, 'g', -1, 64)
	default:
		return nil, false
	}
	result, ok := new(big.Rat).SetString(text)
	return result, ok
}
