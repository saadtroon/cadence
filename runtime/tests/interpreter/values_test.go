package interpreter

import (
	"flag"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/atree"

	"github.com/onflow/cadence/runtime/ast"
	"github.com/onflow/cadence/runtime/common"
	"github.com/onflow/cadence/runtime/interpreter"
	"github.com/onflow/cadence/runtime/sema"
	"github.com/onflow/cadence/runtime/tests/utils"
)

// TODO: make these program args?
const containerMaxDepth = 3
const containerMaxSize = 1_000
const innerContainerMaxSize = 300
const compositeMaxFields = 10

var runSmokeTests = flag.Bool("runSmokeTests", false, "Run smoke tests on values")

func TestRandomMapOperations(t *testing.T) {
	if !*runSmokeTests {
		t.SkipNow()
	}

	seed := time.Now().UnixNano()
	fmt.Printf("Seed used for map opearations test: %d \n", seed)
	rand.Seed(seed)

	storage := interpreter.NewInMemoryStorage()
	inter, err := interpreter.NewInterpreter(
		&interpreter.Program{
			Program:     ast.NewProgram([]ast.Declaration{}),
			Elaboration: sema.NewElaboration(),
		},
		utils.TestLocation,
		interpreter.WithStorage(storage),
		interpreter.WithImportLocationHandler(
			func(inter *interpreter.Interpreter, location common.Location) interpreter.Import {
				return interpreter.VirtualImport{
					Elaboration: inter.Program.Elaboration,
				}
			},
		),
	)
	require.NoError(t, err)

	numberOfValues := randomInt(containerMaxSize)

	var testMap, copyOfTestMap *interpreter.DictionaryValue
	var storageSize, slabCounts int

	entries := newValueMap(numberOfValues)
	orgOwner := common.Address{'A'}

	t.Run("construction", func(t *testing.T) {
		keyValues := make([]interpreter.Value, numberOfValues*2)
		for i := 0; i < numberOfValues; i++ {
			key := randomHashableValue(inter, orgOwner)
			value := randomStorableValue(inter, orgOwner, 0)

			entries.put(inter, key, value)

			keyValues[i*2] = key
			keyValues[i*2+1] = value
		}

		testMap = interpreter.NewDictionaryValueWithAddress(
			inter,
			interpreter.DictionaryStaticType{
				KeyType:   interpreter.PrimitiveStaticTypeAnyStruct,
				ValueType: interpreter.PrimitiveStaticTypeAnyStruct,
			},
			orgOwner,
			keyValues...,
		)

		storageSize, slabCounts = getSlabStorageSize(t, storage)

		require.Equal(t, testMap.Count(), entries.size())

		entries.foreach(func(orgKey, orgValue interpreter.Value) (exit bool) {
			exists := testMap.ContainsKey(inter, interpreter.ReturnEmptyLocationRange, orgKey)
			require.True(t, bool(exists))

			value, found := testMap.Get(inter, interpreter.ReturnEmptyLocationRange, orgKey)
			require.True(t, found)
			utils.AssertValuesEqual(t, inter, orgValue, value)

			return false
		})

		owner := testMap.GetOwner()
		assert.Equal(t, orgOwner[:], owner[:])
	})

	t.Run("iterate", func(t *testing.T) {
		require.Equal(t, testMap.Count(), entries.size())

		testMap.Iterate(func(key, value interpreter.Value) (resume bool) {
			orgValue, ok := entries.get(key)
			require.True(t, ok, "cannot fine key: %v", key)

			utils.AssertValuesEqual(t, inter, orgValue, value)
			return true
		})
	})

	t.Run("deep copy", func(t *testing.T) {
		newOwner := atree.Address([8]byte{'B'})
		copyOfTestMap = testMap.Transfer(
			inter,
			interpreter.ReturnEmptyLocationRange,
			newOwner,
			false,
			nil,
		).(*interpreter.DictionaryValue)

		require.Equal(t, entries.size(), copyOfTestMap.Count())

		entries.foreach(func(orgKey, orgValue interpreter.Value) (exit bool) {
			exists := copyOfTestMap.ContainsKey(inter, interpreter.ReturnEmptyLocationRange, orgKey)
			require.True(t, bool(exists))

			value, found := copyOfTestMap.Get(inter, interpreter.ReturnEmptyLocationRange, orgKey)
			require.True(t, found)
			utils.AssertValuesEqual(t, inter, orgValue, value)

			return false
		})

		owner := copyOfTestMap.GetOwner()
		assert.Equal(t, newOwner[:], owner[:])
	})

	t.Run("deep remove", func(t *testing.T) {
		copyOfTestMap.DeepRemove(inter)
		err = storage.Remove(copyOfTestMap.StorageID())
		require.NoError(t, err)

		// deep removal should clean up everything
		newStorageSize, newSlabCounts := getSlabStorageSize(t, storage)
		assert.Equal(t, slabCounts, newSlabCounts)
		assert.Equal(t, storageSize, newStorageSize)

		require.Equal(t, entries.size(), testMap.Count())

		// go over original values again and check no missing data (no side effect should be found)
		entries.foreach(func(orgKey, orgValue interpreter.Value) (exit bool) {
			exists := testMap.ContainsKey(inter, interpreter.ReturnEmptyLocationRange, orgKey)
			require.True(t, bool(exists))

			value, found := testMap.Get(inter, interpreter.ReturnEmptyLocationRange, orgKey)
			require.True(t, found)
			utils.AssertValuesEqual(t, inter, orgValue, value)

			return false
		})

		owner := testMap.GetOwner()
		assert.Equal(t, orgOwner[:], owner[:])
	})

	t.Run("insert", func(t *testing.T) {
		newEntries := newValueMap(numberOfValues)

		dictionary := interpreter.NewDictionaryValueWithAddress(
			inter,
			interpreter.DictionaryStaticType{
				KeyType:   interpreter.PrimitiveStaticTypeAnyStruct,
				ValueType: interpreter.PrimitiveStaticTypeAnyStruct,
			},
			orgOwner,
		)

		storageSize, slabCounts = getSlabStorageSize(t, storage)

		// Insert
		for i := 0; i < numberOfValues; i++ {
			key := randomHashableValue(inter, orgOwner)
			value := randomStorableValue(inter, orgOwner, 0)

			newEntries.put(inter, key, value)

			_ = dictionary.Insert(inter, interpreter.ReturnEmptyLocationRange, key, value)
		}

		require.Equal(t, newEntries.size(), dictionary.Count())

		// Go over original values again and check no missing data (no side effect should be found)
		newEntries.foreach(func(orgKey, orgValue interpreter.Value) (exit bool) {
			exists := dictionary.ContainsKey(inter, interpreter.ReturnEmptyLocationRange, orgKey)
			require.True(t, bool(exists))

			value, found := dictionary.Get(inter, interpreter.ReturnEmptyLocationRange, orgKey)
			require.True(t, found)
			utils.AssertValuesEqual(t, inter, orgValue, value)

			return false
		})
	})

	t.Run("remove", func(t *testing.T) {
		newEntries := newValueMap(numberOfValues)

		keyValues := make([][2]interpreter.Value, numberOfValues)
		for i := 0; i < numberOfValues; i++ {
			key := randomHashableValue(inter, orgOwner)
			value := randomStorableValue(inter, orgOwner, 0)

			newEntries.put(inter, key, value)

			keyValues[i][0] = key
			keyValues[i][1] = value
		}

		dictionary := interpreter.NewDictionaryValueWithAddress(
			inter,
			interpreter.DictionaryStaticType{
				KeyType:   interpreter.PrimitiveStaticTypeAnyStruct,
				ValueType: interpreter.PrimitiveStaticTypeAnyStruct,
			},
			orgOwner,
		)

		require.Equal(t, 0, dictionary.Count())

		// Get the initial storage size before inserting values
		startingStorageSize, startingSlabCounts := getSlabStorageSize(t, storage)

		// Insert
		for _, keyValue := range keyValues {
			dictionary.Insert(inter, interpreter.ReturnEmptyLocationRange, keyValue[0], keyValue[1])
		}

		require.Equal(t, newEntries.size(), dictionary.Count())

		// Remove
		newEntries.foreach(func(orgKey, orgValue interpreter.Value) (exit bool) {
			removedValue := dictionary.Remove(inter, interpreter.ReturnEmptyLocationRange, orgKey)

			assert.IsType(t, &interpreter.SomeValue{}, removedValue)
			someValue := removedValue.(*interpreter.SomeValue)

			// Removed value must be same as the original value
			utils.AssertValuesEqual(t, inter, orgValue, someValue.Value)

			return false
		})

		// Dictionary must be empty
		require.Equal(t, 0, dictionary.Count())

		storageSize, slabCounts := getSlabStorageSize(t, storage)

		// Storage size after removals should be same as the size before insertion.
		assert.Equal(t, startingStorageSize, storageSize)
		assert.Equal(t, startingSlabCounts, slabCounts)
	})

	t.Run("remove enum", func(t *testing.T) {

		const elementCount = 1

		// Step 1: Create a new dictionary
		// Slab count is 1
		dictionary := interpreter.NewDictionaryValueWithAddress(
			inter,
			interpreter.DictionaryStaticType{
				KeyType:   interpreter.PrimitiveStaticTypeAnyStruct,
				ValueType: interpreter.PrimitiveStaticTypeAnyStruct,
			},
			orgOwner,
		)

		require.Equal(t, 0, dictionary.Count())

		// Get the initial storage size after creating empty dictionary
		startingStorageSize, startingSlabCounts := getSlabStorageSize(t, storage)

		newEntries := newValueMap(elementCount)

		// Step 2: Create enum value as key
		// Slab count is 3
		keyValues := make([][2]interpreter.Value, elementCount)
		for i := 0; i < elementCount; i++ {
			// Create a random enum as key
			key := generateRandomHashableValue(inter, orgOwner, Enum)
			value := interpreter.VoidValue{}

			newEntries.put(inter, key, value)

			keyValues[i][0] = key
			keyValues[i][1] = value
		}

		// Step 3: Insert
		// Slab count is 4
		for _, keyValue := range keyValues {
			dictionary.Insert(inter, interpreter.ReturnEmptyLocationRange, keyValue[0], keyValue[1])
		}

		// Step 4: Remove
		// After all elements are remove, slab count is 3
		newEntries.foreach(func(orgKey, orgValue interpreter.Value) (exit bool) {
			removedValue := dictionary.Remove(inter, interpreter.ReturnEmptyLocationRange, orgKey)

			assert.IsType(t, &interpreter.SomeValue{}, removedValue)
			someValue := removedValue.(*interpreter.SomeValue)

			// Removed value must be same as the original value
			utils.AssertValuesEqual(t, inter, orgValue, someValue.Value)

			return false
		})

		// Dictionary must be empty
		require.Equal(t, 0, dictionary.Count())

		storageSize, slabCounts = getSlabStorageSize(t, storage)

		// Storage size after removals should be same as the size before insertion.
		assert.Equal(t, startingStorageSize, storageSize)
		assert.Equal(t, startingSlabCounts, slabCounts)
	})
}

func TestRandomArrayOperations(t *testing.T) {
	if !*runSmokeTests {
		t.SkipNow()
	}

	seed := time.Now().UnixNano()
	fmt.Printf("Seed used for array opearations test: %d \n", seed)
	rand.Seed(seed)

	storage := interpreter.NewInMemoryStorage()
	inter, err := interpreter.NewInterpreter(
		&interpreter.Program{
			Program:     ast.NewProgram([]ast.Declaration{}),
			Elaboration: sema.NewElaboration(),
		},
		utils.TestLocation,
		interpreter.WithStorage(storage),
		interpreter.WithImportLocationHandler(
			func(inter *interpreter.Interpreter, location common.Location) interpreter.Import {
				return interpreter.VirtualImport{
					Elaboration: inter.Program.Elaboration,
				}
			},
		),
	)
	require.NoError(t, err)

	numberOfValues := randomInt(containerMaxSize)

	var testArray, copyOfTestArray *interpreter.ArrayValue
	var storageSize, slabCounts int

	elements := make([]interpreter.Value, numberOfValues)
	orgOwner := common.Address{'A'}

	values := make([]interpreter.Value, numberOfValues)

	t.Run("construction", func(t *testing.T) {
		for i := 0; i < numberOfValues; i++ {
			value := randomStorableValue(inter, orgOwner, 0)
			elements[i] = value
			values[i] = deepCopyValue(inter, value)
		}

		testArray = interpreter.NewArrayValue(
			inter,
			interpreter.VariableSizedStaticType{
				Type: interpreter.PrimitiveStaticTypeAnyStruct,
			},
			orgOwner,
			values...,
		)

		storageSize, slabCounts = getSlabStorageSize(t, storage)

		require.Equal(t, len(elements), testArray.Count())

		for index, orgElement := range elements {
			element := testArray.Get(inter, interpreter.ReturnEmptyLocationRange, index)
			utils.AssertValuesEqual(t, inter, orgElement, element)
		}

		owner := testArray.GetOwner()
		assert.Equal(t, orgOwner[:], owner[:])
	})

	t.Run("iterate", func(t *testing.T) {
		require.Equal(t, testArray.Count(), len(elements))

		index := 0
		testArray.Iterate(func(element interpreter.Value) (resume bool) {
			orgElement := elements[index]
			utils.AssertValuesEqual(t, inter, orgElement, element)

			elementByIndex := testArray.Get(inter, interpreter.ReturnEmptyLocationRange, index)
			utils.AssertValuesEqual(t, inter, element, elementByIndex)

			index++
			return true
		})
	})

	t.Run("deep copy", func(t *testing.T) {
		newOwner := atree.Address([8]byte{'B'})
		copyOfTestArray = testArray.Transfer(
			inter,
			interpreter.ReturnEmptyLocationRange,
			newOwner,
			false,
			nil,
		).(*interpreter.ArrayValue)

		require.Equal(t, len(elements), copyOfTestArray.Count())

		for index, orgElement := range elements {
			element := copyOfTestArray.Get(inter, interpreter.ReturnEmptyLocationRange, index)
			utils.AssertValuesEqual(t, inter, orgElement, element)
		}

		owner := copyOfTestArray.GetOwner()
		assert.Equal(t, newOwner[:], owner[:])
	})

	t.Run("deep removal", func(t *testing.T) {
		copyOfTestArray.DeepRemove(inter)
		err = storage.Remove(copyOfTestArray.StorageID())
		require.NoError(t, err)

		// deep removal should clean up everything
		newStorageSize, newSlabCounts := getSlabStorageSize(t, storage)
		assert.Equal(t, slabCounts, newSlabCounts)
		assert.Equal(t, storageSize, newStorageSize)

		assert.Equal(t, len(elements), testArray.Count())

		// go over original elements again and check no missing data (no side effect should be found)
		for index, orgElement := range elements {
			element := testArray.Get(inter, interpreter.ReturnEmptyLocationRange, index)
			utils.AssertValuesEqual(t, inter, orgElement, element)
		}

		owner := testArray.GetOwner()
		assert.Equal(t, orgOwner[:], owner[:])
	})
}

func TestRandomCompositeValueOperations(t *testing.T) {
	if !*runSmokeTests {
		t.SkipNow()
	}

	seed := time.Now().UnixNano()
	fmt.Printf("Seed used for compsoite opearations test: %d \n", seed)
	rand.Seed(seed)

	storage := interpreter.NewInMemoryStorage()
	inter, err := interpreter.NewInterpreter(
		&interpreter.Program{
			Program:     ast.NewProgram([]ast.Declaration{}),
			Elaboration: sema.NewElaboration(),
		},
		utils.TestLocation,
		interpreter.WithStorage(storage),
		interpreter.WithImportLocationHandler(
			func(inter *interpreter.Interpreter, location common.Location) interpreter.Import {
				return interpreter.VirtualImport{
					Elaboration: inter.Program.Elaboration,
				}
			},
		),
	)
	require.NoError(t, err)

	var testComposite, copyOfTestComposite *interpreter.CompositeValue
	var storageSize, slabCounts int

	fieldsCount := randomInt(compositeMaxFields)
	orgFields := make(map[string]interpreter.Value, fieldsCount)

	orgOwner := common.Address{'A'}

	t.Run("construction", func(t *testing.T) {
		identifier := randomUTF8String()

		location := common.AddressLocation{
			Address: orgOwner,
			Name:    identifier,
		}

		fields := make([]interpreter.CompositeField, fieldsCount)

		fieldNames := make(map[string]interface{}, fieldsCount)

		for i := 0; i < fieldsCount; {
			fieldName := randomUTF8String()

			// avoid duplicate field names
			if _, ok := fieldNames[fieldName]; ok {
				continue
			}
			fieldNames[fieldName] = struct{}{}

			field := interpreter.CompositeField{
				Name:  string(fieldName),
				Value: randomStorableValue(inter, orgOwner, 0),
			}

			fields[i] = field
			orgFields[field.Name] = deepCopyValue(inter, field.Value)

			i++
		}

		kind := common.CompositeKindStructure

		compositeType := &sema.CompositeType{
			Location:   location,
			Identifier: identifier,
			Kind:       kind,
		}

		compositeType.Members = sema.NewStringMemberOrderedMap()
		for _, field := range fields {
			compositeType.Members.Set(
				field.Name,
				sema.NewPublicConstantFieldMember(
					compositeType,
					field.Name,
					sema.AnyStructType,
					"",
				),
			)
		}

		// Add the type to the elaboration, to short-circuit the type-lookup
		inter.Program.Elaboration.CompositeTypes[compositeType.ID()] = compositeType

		testComposite = interpreter.NewCompositeValue(
			inter,
			location,
			identifier,
			kind,
			fields,
			orgOwner,
		)

		storageSize, slabCounts = getSlabStorageSize(t, storage)

		for fieldName, orgFieldValue := range orgFields {
			fieldValue := testComposite.GetField(inter, interpreter.ReturnEmptyLocationRange, fieldName)
			utils.AssertValuesEqual(t, inter, orgFieldValue, fieldValue)
		}

		owner := testComposite.GetOwner()
		assert.Equal(t, orgOwner[:], owner[:])
	})

	t.Run("iterate", func(t *testing.T) {
		fieldCount := 0
		testComposite.ForEachField(func(name string, value interpreter.Value) {
			orgValue, ok := orgFields[name]
			require.True(t, ok)
			utils.AssertValuesEqual(t, inter, orgValue, value)
			fieldCount++
		})

		assert.Equal(t, len(orgFields), fieldCount)
	})

	t.Run("deep copy", func(t *testing.T) {
		newOwner := atree.Address([8]byte{'B'})

		copyOfTestComposite = testComposite.Transfer(
			inter,
			interpreter.ReturnEmptyLocationRange,
			newOwner,
			false,
			nil,
		).(*interpreter.CompositeValue)

		for name, orgValue := range orgFields {
			value := copyOfTestComposite.GetField(inter, interpreter.ReturnEmptyLocationRange, name)
			utils.AssertValuesEqual(t, inter, orgValue, value)
		}

		owner := copyOfTestComposite.GetOwner()
		assert.Equal(t, newOwner[:], owner[:])
	})

	t.Run("deep remove", func(t *testing.T) {
		copyOfTestComposite.DeepRemove(inter)
		err = storage.Remove(copyOfTestComposite.StorageID())
		require.NoError(t, err)

		// deep removal should clean up everything
		newStorageSize, newSlabCounts := getSlabStorageSize(t, storage)
		assert.Equal(t, slabCounts, newSlabCounts)
		assert.Equal(t, storageSize, newStorageSize)

		// go over original values again and check no missing data (no side effect should be found)
		for name, orgValue := range orgFields {
			value := testComposite.GetField(inter, interpreter.ReturnEmptyLocationRange, name)
			utils.AssertValuesEqual(t, inter, orgValue, value)
		}

		owner := testComposite.GetOwner()
		assert.Equal(t, orgOwner[:], owner[:])
	})

	t.Run("remove field", func(t *testing.T) {
		newOwner := atree.Address([8]byte{'c'})

		composite := testComposite.Transfer(
			inter,
			interpreter.ReturnEmptyLocationRange,
			newOwner,
			false,
			nil,
		).(*interpreter.CompositeValue)

		require.NoError(t, err)

		for name, _ := range orgFields {
			composite.RemoveField(inter, interpreter.ReturnEmptyLocationRange, name)
			value := composite.GetField(inter, interpreter.ReturnEmptyLocationRange, name)
			assert.Nil(t, value)
		}
	})
}

func getSlabStorageSize(t *testing.T, storage interpreter.InMemoryStorage) (totalSize int, slabCounts int) {
	slabs, err := storage.Encode()
	require.NoError(t, err)

	for _, slab := range slabs {
		totalSize += len(slab)
		slabCounts++
	}

	return
}

// deepCopyValue deep copies values at a higher level
func deepCopyValue(inter *interpreter.Interpreter, value interpreter.Value) interpreter.Value {
	switch v := value.(type) {

	// Int
	case interpreter.IntValue:
		var n big.Int
		n.Set(v.BigInt)
		return interpreter.NewIntValueFromBigInt(&n)
	case interpreter.Int8Value:
		return interpreter.Int8Value(int8(v))
	case interpreter.Int16Value:
		return interpreter.Int16Value(int16(v))
	case interpreter.Int32Value:
		return interpreter.Int32Value(int32(v))
	case interpreter.Int64Value:
		return interpreter.Int64Value(int64(v))
	case interpreter.Int128Value:
		var n big.Int
		n.Set(v.BigInt)
		return interpreter.NewInt128ValueFromBigInt(&n)
	case interpreter.Int256Value:
		var n big.Int
		n.Set(v.BigInt)
		return interpreter.NewInt256ValueFromBigInt(&n)

	// Uint
	case interpreter.UIntValue:
		var n big.Int
		n.Set(v.BigInt)
		return interpreter.NewUIntValueFromBigInt(&n)
	case interpreter.UInt8Value:
		return interpreter.UInt8Value(uint8(v))
	case interpreter.UInt16Value:
		return interpreter.UInt16Value(uint16(v))
	case interpreter.UInt32Value:
		return interpreter.UInt32Value(uint32(v))
	case interpreter.UInt64Value:
		return interpreter.UInt64Value(uint64(v))
	case interpreter.UInt128Value:
		var n big.Int
		n.Set(v.BigInt)
		return interpreter.NewUInt128ValueFromBigInt(&n)
	case interpreter.UInt256Value:
		var n big.Int
		n.Set(v.BigInt)
		return interpreter.NewUInt256ValueFromBigInt(&n)

	case interpreter.Word8Value,
		interpreter.Word16Value,
		interpreter.Word32Value,
		interpreter.Word64Value:
		return v

	case *interpreter.StringValue:
		b := []byte(v.Str)
		data := make([]byte, len(b))
		copy(data, b)
		return interpreter.NewStringValue(string(data))

	case interpreter.AddressValue:
		b := v[:]
		data := make([]byte, len(b))
		copy(data, b)
		return interpreter.NewAddressValueFromBytes(data)
	case interpreter.Fix64Value:
		return interpreter.NewFix64ValueWithInteger(int64(v.ToInt()))
	case interpreter.UFix64Value:
		return interpreter.NewUFix64ValueWithInteger(uint64(v.ToInt()))

	case interpreter.PathValue:
		return interpreter.PathValue{
			Domain:     v.Domain,
			Identifier: v.Identifier,
		}

	case interpreter.BoolValue:
		return v

	case interpreter.VoidValue:
		return interpreter.VoidValue{}

	case *interpreter.DictionaryValue:
		keyValues := make([]interpreter.Value, 0, v.Count()*2)
		v.Iterate(func(key, value interpreter.Value) (resume bool) {
			keyValues = append(keyValues, deepCopyValue(inter, key))
			keyValues = append(keyValues, deepCopyValue(inter, value))
			return true
		})

		return interpreter.NewDictionaryValueWithAddress(
			inter,
			interpreter.DictionaryStaticType{
				KeyType:   v.Type.KeyType,
				ValueType: v.Type.ValueType,
			},
			v.GetOwner(),
			keyValues...,
		)
	case *interpreter.ArrayValue:
		elements := make([]interpreter.Value, 0, v.Count())
		v.Iterate(func(value interpreter.Value) (resume bool) {
			elements = append(elements, deepCopyValue(inter, value))
			return true
		})

		return interpreter.NewArrayValue(
			inter,
			v.Type,
			v.GetOwner(),
			elements...,
		)
	case *interpreter.CompositeValue:
		fields := make([]interpreter.CompositeField, 0)
		v.ForEachField(func(name string, value interpreter.Value) {
			fields = append(fields, interpreter.CompositeField{
				Name:  name,
				Value: deepCopyValue(inter, value),
			})
		})

		return interpreter.NewCompositeValue(
			inter,
			v.Location,
			v.QualifiedIdentifier,
			v.Kind,
			fields,
			v.GetOwner(),
		)

	case *interpreter.CapabilityValue:
		return &interpreter.CapabilityValue{
			Address:    deepCopyValue(inter, v.Address).(interpreter.AddressValue),
			Path:       deepCopyValue(inter, v.Path).(interpreter.PathValue),
			BorrowType: v.BorrowType,
		}
	case *interpreter.SomeValue:
		return interpreter.NewSomeValueNonCopying(deepCopyValue(inter, v.Value))
	case interpreter.NilValue:
		return interpreter.NilValue{}
	default:
		panic("unreachable")
	}
}

func randomStorableValue(inter *interpreter.Interpreter, owner common.Address, currentDepth int) interpreter.Value {
	n := 0
	if currentDepth < containerMaxDepth {
		n = randomInt(Composite)
	} else {
		n = randomInt(Capability)
	}

	switch n {

	// Non-hashable
	case Void:
		return interpreter.VoidValue{}
	case Nil:
		return interpreter.NilValue{}
	case Dictionary_1, Dictionary_2:
		return randomDictionaryValue(inter, owner, currentDepth)
	case Array_1, Array_2:
		return randomArrayValue(inter, owner, currentDepth)
	case Composite:
		return randomCompositeValue(inter, common.CompositeKindStructure, owner, currentDepth)
	case Capability:
		return &interpreter.CapabilityValue{
			Address: randomAddressValue(),
			Path:    randomPathValue(),
			BorrowType: interpreter.ReferenceStaticType{
				Authorized: false,
				Type:       interpreter.PrimitiveStaticTypeAnyStruct,
			},
		}
	case Some:
		return &interpreter.SomeValue{
			Value: randomStorableValue(inter, owner, currentDepth+1),
		}

	// Hashable
	default:
		return generateRandomHashableValue(inter, owner, n)
	}
}

func randomHashableValue(interpreter *interpreter.Interpreter, owner common.Address) interpreter.Value {
	return generateRandomHashableValue(interpreter, owner, randomInt(Enum))
}

func generateRandomHashableValue(inter *interpreter.Interpreter, owner common.Address, n int) interpreter.Value {
	switch n {

	// Int
	case Int:
		return interpreter.NewIntValueFromInt64(int64(sign()) * rand.Int63())
	case Int8:
		return interpreter.Int8Value(randomInt(math.MaxUint8))
	case Int16:
		return interpreter.Int16Value(randomInt(math.MaxUint16))
	case Int32:
		return interpreter.Int32Value(int32(sign()) * rand.Int31())
	case Int64:
		return interpreter.Int64Value(int64(sign()) * rand.Int63())
	case Int128:
		return interpreter.NewInt128ValueFromInt64(int64(sign()) * rand.Int63())
	case Int256:
		return interpreter.NewInt256ValueFromInt64(int64(sign()) * rand.Int63())

	// UInt
	case UInt:
		return interpreter.NewUIntValueFromUint64(rand.Uint64())
	case UInt8:
		return interpreter.UInt8Value(randomInt(math.MaxUint8))
	case UInt16:
		return interpreter.UInt16Value(randomInt(math.MaxUint16))
	case UInt32:
		return interpreter.UInt32Value(rand.Uint32())
	case UInt64_1, UInt64_2, UInt64_3, UInt64_4: // should be more common
		return interpreter.UInt64Value(rand.Uint64())
	case UInt128:
		return interpreter.NewUInt128ValueFromUint64(rand.Uint64())
	case UInt256:
		return interpreter.NewUInt256ValueFromUint64(rand.Uint64())

	// Word
	case Word8:
		return interpreter.Word8Value(randomInt(math.MaxUint8))
	case Word16:
		return interpreter.Word16Value(randomInt(math.MaxUint16))
	case Word32:
		return interpreter.Word32Value(rand.Uint32())
	case Word64:
		return interpreter.Word64Value(rand.Uint64())

	// Fixed point
	case Fix64:
		return interpreter.NewFix64ValueWithInteger(int64(sign()) * rand.Int63n(sema.Fix64TypeMaxInt))
	case UFix64:
		return interpreter.NewUFix64ValueWithInteger(
			uint64(rand.Int63n(
				int64(sema.UFix64TypeMaxInt),
			)),
		)

	// String
	case String_1, String_2, String_3, String_4: // small string - should be more common
		size := randomInt(255)
		return interpreter.NewStringValue(randomUTF8StringOfSize(size))
	case String_5: // large string
		size := randomInt(4048) + 255
		return interpreter.NewStringValue(randomUTF8StringOfSize(size))

	case Bool_True:
		return interpreter.BoolValue(true)
	case Bool_False:
		return interpreter.BoolValue(false)

	case Address:
		return randomAddressValue()

	case Path:
		return randomPathValue()

	case Enum:
		// Get a random integer subtype to be used as the raw-type of enum
		typ := randomInt(Word64)

		rawValue := generateRandomHashableValue(inter, owner, typ).(interpreter.NumberValue)

		identifier := randomUTF8String()

		address := make([]byte, 8)
		rand.Read(address)

		location := common.AddressLocation{
			Address: common.BytesToAddress(address),
			Name:    identifier,
		}

		enumType := &sema.CompositeType{
			Identifier:  identifier,
			EnumRawType: intSubtype(typ),
			Kind:        common.CompositeKindEnum,
			Location:    location,
		}

		inter.Program.Elaboration.CompositeTypes[enumType.ID()] = enumType

		enum := interpreter.NewCompositeValue(
			inter,
			location,
			enumType.QualifiedIdentifier(),
			enumType.Kind,
			[]interpreter.CompositeField{
				{
					Name:  sema.EnumRawValueFieldName,
					Value: rawValue,
				},
			},
			owner,
		)

		if enum.GetField(nil, interpreter.ReturnEmptyLocationRange, sema.EnumRawValueFieldName) == nil {
			panic("enum without raw value")
		}

		return enum

	default:
		panic(fmt.Sprintf("unsupported:  %d", n))
	}
}

func sign() int {
	if randomInt(1) == 1 {
		return 1
	}

	return -1
}

func randomAddressValue() interpreter.AddressValue {
	data := make([]byte, 8)
	rand.Read(data)
	return interpreter.NewAddressValueFromBytes(data)
}

func randomPathValue() interpreter.PathValue {
	randomDomain := rand.Intn(len(common.AllPathDomains))
	identifier := randomUTF8String()

	return interpreter.PathValue{
		Domain:     common.AllPathDomains[randomDomain],
		Identifier: identifier,
	}
}

func randomDictionaryValue(
	inter *interpreter.Interpreter,
	owner common.Address,
	currentDepth int,
) interpreter.Value {

	entryCount := randomInt(innerContainerMaxSize)
	keyValues := make([]interpreter.Value, entryCount*2)

	for i := 0; i < entryCount; i++ {
		key := randomHashableValue(inter, owner)
		value := randomStorableValue(inter, owner, currentDepth+1)
		keyValues[i*2] = key
		keyValues[i*2+1] = value
	}

	return interpreter.NewDictionaryValueWithAddress(
		inter,
		interpreter.DictionaryStaticType{
			KeyType:   interpreter.PrimitiveStaticTypeAnyStruct,
			ValueType: interpreter.PrimitiveStaticTypeAnyStruct,
		},
		owner,
		keyValues...,
	)
}

func randomInt(upperBound int) int {
	return rand.Intn(upperBound + 1)
}

func randomArrayValue(inter *interpreter.Interpreter, owner common.Address, currentDepth int) interpreter.Value {
	elementsCount := randomInt(innerContainerMaxSize)
	elements := make([]interpreter.Value, elementsCount)

	for i := 0; i < elementsCount; i++ {
		value := randomStorableValue(inter, owner, currentDepth+1)
		elements[i] = deepCopyValue(inter, value)
	}

	return interpreter.NewArrayValue(
		inter,
		interpreter.VariableSizedStaticType{
			Type: interpreter.PrimitiveStaticTypeAnyStruct,
		},
		owner,
		elements...,
	)
}

func randomCompositeValue(
	inter *interpreter.Interpreter,
	kind common.CompositeKind,
	owner common.Address,
	currentDepth int,
) interpreter.Value {

	identifier := randomUTF8String()

	address := make([]byte, 8)
	rand.Read(address)

	location := common.AddressLocation{
		Address: common.BytesToAddress(address),
		Name:    identifier,
	}

	fieldsCount := randomInt(compositeMaxFields)
	fields := make([]interpreter.CompositeField, fieldsCount)

	for i := 0; i < fieldsCount; i++ {
		fieldName := randomUTF8String()

		fields[i] = interpreter.CompositeField{
			Name:  fieldName,
			Value: randomStorableValue(inter, owner, currentDepth+1),
		}
	}

	compositeType := &sema.CompositeType{
		Location:   location,
		Identifier: identifier,
		Kind:       kind,
	}

	compositeType.Members = sema.NewStringMemberOrderedMap()
	for _, field := range fields {
		compositeType.Members.Set(
			field.Name,
			sema.NewPublicConstantFieldMember(
				compositeType,
				field.Name,
				sema.AnyStructType, // TODO: handle resources
				"",
			),
		)
	}

	// Add the type to the elaboration, to short-circuit the type-lookup
	inter.Program.Elaboration.CompositeTypes[compositeType.ID()] = compositeType

	return interpreter.NewCompositeValue(
		inter,
		location,
		identifier,
		kind,
		fields,
		owner,
	)
}

func intSubtype(n int) sema.Type {
	switch n {
	// Int
	case Int:
		return sema.IntType
	case Int8:
		return sema.Int8Type
	case Int16:
		return sema.Int16Type
	case Int32:
		return sema.Int32Type
	case Int64:
		return sema.Int64Type
	case Int128:
		return sema.Int128Type
	case Int256:
		return sema.Int256Type

	// UInt
	case UInt:
		return sema.UIntType
	case UInt8:
		return sema.UInt8Type
	case UInt16:
		return sema.UInt16Type
	case UInt32:
		return sema.UInt32Type
	case UInt64_1, UInt64_2, UInt64_3, UInt64_4:
		return sema.UInt64Type
	case UInt128:
		return sema.UInt128Type
	case UInt256:
		return sema.UInt256Type

	// Word
	case Word8:
		return sema.Word8Type
	case Word16:
		return sema.Word16Type
	case Word32:
		return sema.Word32Type
	case Word64:
		return sema.Word64Type

	default:
		panic(fmt.Sprintf("unsupported:  %d", n))
	}
}

const (
	// Hashable values
	Int = iota
	Int8
	Int16
	Int32
	Int64
	Int128
	Int256

	UInt
	UInt8
	UInt16
	UInt32
	UInt64_1
	UInt64_2
	UInt64_3
	UInt64_4
	UInt128
	UInt256

	Word8
	Word16
	Word32
	Word64

	Fix64
	UFix64

	String_1
	String_2
	String_3
	String_4
	String_5

	Bool_True
	Bool_False
	Path
	Address
	Enum

	// Non-hashable values

	Void
	Nil // `Never?`
	Capability

	// Containers
	Some
	Array_1
	Array_2
	Dictionary_1
	Dictionary_2
	Composite
)

type valueMap struct {
	values map[interface{}]interpreter.Value
	keys   map[interface{}]interpreter.Value
}

func newValueMap(size int) *valueMap {
	return &valueMap{
		values: make(map[interface{}]interpreter.Value, size),
		keys:   make(map[interface{}]interpreter.Value, size),
	}
}

type enumKey struct {
	location            common.Location
	qualifiedIdentifier string
	kind                common.CompositeKind
	rawValue            interpreter.Value
}

func (m *valueMap) put(inter *interpreter.Interpreter, key, value interpreter.Value) {
	internalKey := m.internalKey(key)

	// Deep copy enum keys. This should be fine since we use an internal key for enums.
	// Deep copying other values would mess key-lookup.
	if _, ok := key.(*interpreter.CompositeValue); ok {
		key = deepCopyValue(inter, key)
	}

	m.keys[internalKey] = key
	m.values[internalKey] = deepCopyValue(inter, value)
}

func (m *valueMap) get(key interpreter.Value) (interpreter.Value, bool) {
	internalKey := m.internalKey(key)
	value, ok := m.values[internalKey]
	return value, ok
}

func (m *valueMap) foreach(apply func(key, value interpreter.Value) (exit bool)) {
	for internalKey, key := range m.keys {
		value := m.values[internalKey]
		exit := apply(key, value)

		if exit {
			return
		}
	}
}

func (m *valueMap) internalKey(key interpreter.Value) interface{} {
	switch key := key.(type) {
	case *interpreter.StringValue:
		return *key
	case *interpreter.CompositeValue:
		return enumKey{
			location:            key.Location,
			qualifiedIdentifier: key.QualifiedIdentifier,
			kind:                key.Kind,
			rawValue: key.GetField(
				nil,
				interpreter.ReturnEmptyLocationRange,
				sema.EnumRawValueFieldName,
			),
		}
	case interpreter.Value:
		return key
	default:
		panic("unreachable")
	}
}

func (m *valueMap) size() int {
	return len(m.keys)
}

func randomUTF8String() string {
	return randomUTF8StringOfSize(8)
}

func randomUTF8StringOfSize(size int) string {
	identifier := make([]byte, size)
	rand.Read(identifier)
	return strings.ToValidUTF8(string(identifier), "$")
}
