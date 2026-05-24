package activity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/melbournecocoa/decanter/model"
)

func TestClassify_FirstSegment_Welcome(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := New("/tmp/test", "", "", "", "", "", "")
	env.RegisterActivity(a.Classify)

	result, err := env.ExecuteActivity(a.Classify, model.ClassifyInput{
		Segment:       model.Segment{Index: 0},
		TotalSegments: 3,
	})
	require.NoError(t, err)

	var output model.ClassifyOutput
	require.NoError(t, result.Get(&output))
	assert.Equal(t, model.SegmentTypeWelcome, output.Type)
}

func TestClassify_LastSegment_WrapUp(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := New("/tmp/test", "", "", "", "", "", "")
	env.RegisterActivity(a.Classify)

	result, err := env.ExecuteActivity(a.Classify, model.ClassifyInput{
		Segment:       model.Segment{Index: 2},
		TotalSegments: 3,
	})
	require.NoError(t, err)

	var output model.ClassifyOutput
	require.NoError(t, result.Get(&output))
	assert.Equal(t, model.SegmentTypeWrapUp, output.Type)
}

func TestClassify_MiddleSegment_Talk(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := New("/tmp/test", "", "", "", "", "", "")
	env.RegisterActivity(a.Classify)

	result, err := env.ExecuteActivity(a.Classify, model.ClassifyInput{
		Segment:       model.Segment{Index: 1},
		TotalSegments: 3,
	})
	require.NoError(t, err)

	var output model.ClassifyOutput
	require.NoError(t, result.Get(&output))
	assert.Equal(t, model.SegmentTypeTalk, output.Type)
}

func TestClassify_SingleSegment_Welcome(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := New("/tmp/test", "", "", "", "", "", "")
	env.RegisterActivity(a.Classify)

	result, err := env.ExecuteActivity(a.Classify, model.ClassifyInput{
		Segment:       model.Segment{Index: 0},
		TotalSegments: 1,
	})
	require.NoError(t, err)

	var output model.ClassifyOutput
	require.NoError(t, result.Get(&output))
	assert.Equal(t, model.SegmentTypeWelcome, output.Type)
}
