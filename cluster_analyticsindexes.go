package gocb

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
)

// AnalyticsIndexManager provides methods for performing Couchbase Analytics index management.
type AnalyticsIndexManager struct {
	aProvider    analyticsIndexQueryProvider
	mgmtProvider mgmtProvider

	globalTimeout time.Duration
	tracer        RequestTracer
}

type analyticsIndexQueryProvider interface {
	AnalyticsQuery(statement string, opts *AnalyticsOptions) (*AnalyticsResult, error)
}

func (am *AnalyticsIndexManager) doAnalyticsQuery(q string, opts *AnalyticsOptions) ([][]byte, error) {
	if opts.Timeout == 0 {
		opts.Timeout = am.globalTimeout
	}

	result, err := am.aProvider.AnalyticsQuery(q, opts)
	if err != nil {
		return nil, err
	}

	var rows [][]byte
	for result.Next() {
		var row json.RawMessage
		err := result.Row(&row)
		if err != nil {
			logWarnf("management operation failed to read row: %s", err)
		} else {
			rows = append(rows, row)
		}
	}
	err = result.Err()
	if err != nil {
		return nil, err
	}

	return rows, nil
}

func (am *AnalyticsIndexManager) doMgmtRequest(req mgmtRequest) (*mgmtResponse, error) {
	resp, err := am.mgmtProvider.executeMgmtRequest(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

type jsonAnalyticsDataset struct {
	DatasetName   string `json:"DatasetName"`
	DataverseName string `json:"DataverseName"`
	LinkName      string `json:"LinkName"`
	BucketName    string `json:"BucketName"`
}

type jsonAnalyticsIndex struct {
	IndexName     string `json:"IndexName"`
	DatasetName   string `json:"DatasetName"`
	DataverseName string `json:"DataverseName"`
	IsPrimary     bool   `json:"IsPrimary"`
}

// AnalyticsDataset contains information about an analytics dataset.
type AnalyticsDataset struct {
	Name          string
	DataverseName string
	LinkName      string
	BucketName    string
}

func (ad *AnalyticsDataset) fromData(data jsonAnalyticsDataset) error {
	ad.Name = data.DatasetName
	ad.DataverseName = data.DataverseName
	ad.LinkName = data.LinkName
	ad.BucketName = data.BucketName

	return nil
}

// AnalyticsIndex contains information about an analytics index.
type AnalyticsIndex struct {
	Name          string
	DatasetName   string
	DataverseName string
	IsPrimary     bool
}

func (ai *AnalyticsIndex) fromData(data jsonAnalyticsIndex) error {
	ai.Name = data.IndexName
	ai.DatasetName = data.DatasetName
	ai.DataverseName = data.DataverseName
	ai.IsPrimary = data.IsPrimary

	return nil
}

// CreateAnalyticsDataverseOptions is the set of options available to the AnalyticsManager CreateDataverse operation.
type CreateAnalyticsDataverseOptions struct {
	IgnoreIfExists bool

	Timeout       time.Duration
	RetryStrategy RetryStrategy
	ParentSpan    RequestSpan
}

// CreateDataverse creates a new analytics dataset.
func (am *AnalyticsIndexManager) CreateDataverse(dataverseName string, opts *CreateAnalyticsDataverseOptions) error {
	if opts == nil {
		opts = &CreateAnalyticsDataverseOptions{}
	}

	if dataverseName == "" {
		return invalidArgumentsError{
			message: "dataset name cannot be empty",
		}
	}

	var ignoreStr string
	if opts.IgnoreIfExists {
		ignoreStr = "IF NOT EXISTS"
	}

	q := fmt.Sprintf("CREATE DATAVERSE `%s` %s", dataverseName, ignoreStr)

	span := createSpan(am.tracer, opts.ParentSpan, "manager_analytics_create_dataverse", "management")
	defer span.End()

	_, err := am.doAnalyticsQuery(q, &AnalyticsOptions{
		Timeout:         opts.Timeout,
		RetryStrategy:   opts.RetryStrategy,
		ParentSpan:      span,
		ClientContextID: uuid.New().String(),
	})
	if err != nil {
		return err
	}

	return nil
}

// DropAnalyticsDataverseOptions is the set of options available to the AnalyticsManager DropDataverse operation.
type DropAnalyticsDataverseOptions struct {
	IgnoreIfNotExists bool

	Timeout       time.Duration
	RetryStrategy RetryStrategy
	ParentSpan    RequestSpan
}

// DropDataverse drops an analytics dataset.
func (am *AnalyticsIndexManager) DropDataverse(dataverseName string, opts *DropAnalyticsDataverseOptions) error {
	if opts == nil {
		opts = &DropAnalyticsDataverseOptions{}
	}
	var ignoreStr string
	if opts.IgnoreIfNotExists {
		ignoreStr = "IF EXISTS"
	}

	q := fmt.Sprintf("DROP DATAVERSE %s %s", dataverseName, ignoreStr)

	span := createSpan(am.tracer, opts.ParentSpan, "manager_analytics_drop_dataverse", "management")
	defer span.End()

	_, err := am.doAnalyticsQuery(q, &AnalyticsOptions{
		Timeout:       opts.Timeout,
		RetryStrategy: opts.RetryStrategy,
		ParentSpan:    span,
	})
	if err != nil {
		return err
	}

	return err
}

// CreateAnalyticsDatasetOptions is the set of options available to the AnalyticsManager CreateDataset operation.
type CreateAnalyticsDatasetOptions struct {
	IgnoreIfExists bool
	Condition      string
	DataverseName  string

	Timeout       time.Duration
	RetryStrategy RetryStrategy
	ParentSpan    RequestSpan
}

// CreateDataset creates a new analytics dataset.
func (am *AnalyticsIndexManager) CreateDataset(datasetName, bucketName string, opts *CreateAnalyticsDatasetOptions) error {
	if opts == nil {
		opts = &CreateAnalyticsDatasetOptions{}
	}

	if datasetName == "" {
		return invalidArgumentsError{
			message: "dataset name cannot be empty",
		}
	}

	var ignoreStr string
	if opts.IgnoreIfExists {
		ignoreStr = "IF NOT EXISTS"
	}

	var where string
	if opts.Condition != "" {
		if !strings.HasPrefix(strings.ToUpper(opts.Condition), "WHERE") {
			where = "WHERE "
		}
		where += opts.Condition
	}

	if opts.DataverseName == "" {
		datasetName = fmt.Sprintf("`%s`", datasetName)
	} else {
		datasetName = fmt.Sprintf("`%s`.`%s`", opts.DataverseName, datasetName)
	}

	q := fmt.Sprintf("CREATE DATASET %s %s ON `%s` %s", ignoreStr, datasetName, bucketName, where)

	span := createSpan(am.tracer, opts.ParentSpan, "manager_analytics_create_dataset", "management")
	defer span.End()

	_, err := am.doAnalyticsQuery(q, &AnalyticsOptions{
		Timeout:       opts.Timeout,
		RetryStrategy: opts.RetryStrategy,
		ParentSpan:    span,
	})
	if err != nil {
		return err
	}

	return nil
}

// DropAnalyticsDatasetOptions is the set of options available to the AnalyticsManager DropDataset operation.
type DropAnalyticsDatasetOptions struct {
	IgnoreIfNotExists bool
	DataverseName     string

	Timeout       time.Duration
	RetryStrategy RetryStrategy
	ParentSpan    RequestSpan
}

// DropDataset drops an analytics dataset.
func (am *AnalyticsIndexManager) DropDataset(datasetName string, opts *DropAnalyticsDatasetOptions) error {
	if opts == nil {
		opts = &DropAnalyticsDatasetOptions{}
	}

	var ignoreStr string
	if opts.IgnoreIfNotExists {
		ignoreStr = "IF EXISTS"
	}

	if opts.DataverseName == "" {
		datasetName = fmt.Sprintf("`%s`", datasetName)
	} else {
		datasetName = fmt.Sprintf("`%s`.`%s`", opts.DataverseName, datasetName)
	}

	q := fmt.Sprintf("DROP DATASET %s %s", datasetName, ignoreStr)

	span := createSpan(am.tracer, opts.ParentSpan, "manager_analytics_drop_dataset", "management")
	defer span.End()

	_, err := am.doAnalyticsQuery(q, &AnalyticsOptions{
		Timeout:       opts.Timeout,
		RetryStrategy: opts.RetryStrategy,
		ParentSpan:    span,
	})
	if err != nil {
		return err
	}

	return nil
}

// GetAllAnalyticsDatasetsOptions is the set of options available to the AnalyticsManager GetAllDatasets operation.
type GetAllAnalyticsDatasetsOptions struct {
	Timeout       time.Duration
	RetryStrategy RetryStrategy
	ParentSpan    RequestSpan
}

// GetAllDatasets gets all analytics datasets.
func (am *AnalyticsIndexManager) GetAllDatasets(opts *GetAllAnalyticsDatasetsOptions) ([]AnalyticsDataset, error) {
	if opts == nil {
		opts = &GetAllAnalyticsDatasetsOptions{}
	}

	q := "SELECT d.* FROM Metadata.`Dataset` d WHERE d.DataverseName <> \"Metadata\""
	span := createSpan(am.tracer, opts.ParentSpan, "manager_analytics_get_all_datasets", "management")
	span.SetAttribute("db.statement", q)
	defer span.End()

	rows, err := am.doAnalyticsQuery(q, &AnalyticsOptions{
		Timeout:       opts.Timeout,
		RetryStrategy: opts.RetryStrategy,
		ParentSpan:    span,
	})
	if err != nil {
		return nil, err
	}

	datasets := make([]AnalyticsDataset, len(rows))
	for rowIdx, row := range rows {
		var datasetData jsonAnalyticsDataset
		err := json.Unmarshal(row, &datasetData)
		if err != nil {
			return nil, err
		}

		err = datasets[rowIdx].fromData(datasetData)
		if err != nil {
			return nil, err
		}
	}

	return datasets, nil
}

// CreateAnalyticsIndexOptions is the set of options available to the AnalyticsManager CreateIndex operation.
type CreateAnalyticsIndexOptions struct {
	IgnoreIfExists bool
	DataverseName  string

	Timeout       time.Duration
	RetryStrategy RetryStrategy
	ParentSpan    RequestSpan
}

// CreateIndex creates a new analytics dataset.
func (am *AnalyticsIndexManager) CreateIndex(datasetName, indexName string, fields map[string]string, opts *CreateAnalyticsIndexOptions) error {
	if opts == nil {
		opts = &CreateAnalyticsIndexOptions{}
	}

	if indexName == "" {
		return invalidArgumentsError{
			message: "index name cannot be empty",
		}
	}
	if len(fields) <= 0 {
		return invalidArgumentsError{
			message: "you must specify at least one field to index",
		}
	}

	var ignoreStr string
	if opts.IgnoreIfExists {
		ignoreStr = "IF NOT EXISTS"
	}

	var indexFields []string
	for name, typ := range fields {
		indexFields = append(indexFields, name+":"+typ)
	}

	if opts.DataverseName == "" {
		datasetName = fmt.Sprintf("`%s`", datasetName)
	} else {
		datasetName = fmt.Sprintf("`%s`.`%s`", opts.DataverseName, datasetName)
	}

	q := fmt.Sprintf("CREATE INDEX `%s` %s ON %s (%s)", indexName, ignoreStr, datasetName, strings.Join(indexFields, ","))

	span := createSpan(am.tracer, opts.ParentSpan, "manager_analytics_create_index", "management")
	defer span.End()

	_, err := am.doAnalyticsQuery(q, &AnalyticsOptions{
		Timeout:       opts.Timeout,
		RetryStrategy: opts.RetryStrategy,
		ParentSpan:    span,
	})
	if err != nil {
		return err
	}

	return nil
}

// DropAnalyticsIndexOptions is the set of options available to the AnalyticsManager DropIndex operation.
type DropAnalyticsIndexOptions struct {
	IgnoreIfNotExists bool
	DataverseName     string

	Timeout       time.Duration
	RetryStrategy RetryStrategy
	ParentSpan    RequestSpan
}

// DropIndex drops an analytics index.
func (am *AnalyticsIndexManager) DropIndex(datasetName, indexName string, opts *DropAnalyticsIndexOptions) error {
	if opts == nil {
		opts = &DropAnalyticsIndexOptions{}
	}

	var ignoreStr string
	if opts.IgnoreIfNotExists {
		ignoreStr = "IF EXISTS"
	}

	if opts.DataverseName == "" {
		datasetName = fmt.Sprintf("`%s`", datasetName)
	} else {
		datasetName = fmt.Sprintf("`%s`.`%s`", opts.DataverseName, datasetName)
	}

	q := fmt.Sprintf("DROP INDEX %s.%s %s", datasetName, indexName, ignoreStr)

	span := createSpan(am.tracer, opts.ParentSpan, "manager_analytics_drop_index", "management")
	span.SetAttribute("db.statement", q)
	defer span.End()

	_, err := am.doAnalyticsQuery(q, &AnalyticsOptions{
		Timeout:       opts.Timeout,
		RetryStrategy: opts.RetryStrategy,
		ParentSpan:    span,
	})
	if err != nil {
		return err
	}

	return nil
}

// GetAllAnalyticsIndexesOptions is the set of options available to the AnalyticsManager GetAllIndexes operation.
type GetAllAnalyticsIndexesOptions struct {
	Timeout       time.Duration
	RetryStrategy RetryStrategy
	ParentSpan    RequestSpan
}

// GetAllIndexes gets all analytics indexes.
func (am *AnalyticsIndexManager) GetAllIndexes(opts *GetAllAnalyticsIndexesOptions) ([]AnalyticsIndex, error) {
	if opts == nil {
		opts = &GetAllAnalyticsIndexesOptions{}
	}

	q := "SELECT d.* FROM Metadata.`Index` d WHERE d.DataverseName <> \"Metadata\""
	span := createSpan(am.tracer, opts.ParentSpan, "manager_analytics_get_all_indexes", "management")
	defer span.End()

	rows, err := am.doAnalyticsQuery(q, &AnalyticsOptions{
		Timeout:       opts.Timeout,
		RetryStrategy: opts.RetryStrategy,
		ParentSpan:    span,
	})
	if err != nil {
		return nil, err
	}

	indexes := make([]AnalyticsIndex, len(rows))
	for rowIdx, row := range rows {
		var indexData jsonAnalyticsIndex
		err := json.Unmarshal(row, &indexData)
		if err != nil {
			return nil, err
		}

		err = indexes[rowIdx].fromData(indexData)
		if err != nil {
			return nil, err
		}
	}

	return indexes, nil
}

// ConnectAnalyticsLinkOptions is the set of options available to the AnalyticsManager ConnectLink operation.
type ConnectAnalyticsLinkOptions struct {
	LinkName string

	Timeout       time.Duration
	RetryStrategy RetryStrategy
	ParentSpan    RequestSpan
}

// ConnectLink connects an analytics link.
func (am *AnalyticsIndexManager) ConnectLink(opts *ConnectAnalyticsLinkOptions) error {
	if opts == nil {
		opts = &ConnectAnalyticsLinkOptions{}
	}

	if opts.LinkName == "" {
		opts.LinkName = "Local"
	}

	q := fmt.Sprintf("CONNECT LINK %s", opts.LinkName)
	span := createSpan(am.tracer, opts.ParentSpan, "manager_analytics_connect_link", "management")
	span.SetAttribute("db.statement", q)
	defer span.End()

	_, err := am.doAnalyticsQuery(q, &AnalyticsOptions{
		Timeout:       opts.Timeout,
		RetryStrategy: opts.RetryStrategy,
		ParentSpan:    span,
	})
	if err != nil {
		return err
	}

	return nil
}

// DisconnectAnalyticsLinkOptions is the set of options available to the AnalyticsManager DisconnectLink operation.
type DisconnectAnalyticsLinkOptions struct {
	LinkName string

	Timeout       time.Duration
	RetryStrategy RetryStrategy
	ParentSpan    RequestSpan
}

// DisconnectLink disconnects an analytics link.
func (am *AnalyticsIndexManager) DisconnectLink(opts *DisconnectAnalyticsLinkOptions) error {
	if opts == nil {
		opts = &DisconnectAnalyticsLinkOptions{}
	}

	if opts.LinkName == "" {
		opts.LinkName = "Local"
	}

	q := fmt.Sprintf("DISCONNECT LINK %s", opts.LinkName)
	span := createSpan(am.tracer, opts.ParentSpan, "manager_analytics_disconnect_link", "management")
	defer span.End()

	_, err := am.doAnalyticsQuery(q, &AnalyticsOptions{
		Timeout:       opts.Timeout,
		RetryStrategy: opts.RetryStrategy,
		ParentSpan:    span,
	})
	if err != nil {
		return err
	}

	return nil
}

// GetPendingMutationsAnalyticsOptions is the set of options available to the user manager GetPendingMutations operation.
type GetPendingMutationsAnalyticsOptions struct {
	Timeout       time.Duration
	RetryStrategy RetryStrategy
	ParentSpan    RequestSpan
}

// GetPendingMutations returns the number of pending mutations for all indexes in the form of dataverse.dataset:mutations.
func (am *AnalyticsIndexManager) GetPendingMutations(opts *GetPendingMutationsAnalyticsOptions) (map[string]map[string]int, error) {
	if opts == nil {
		opts = &GetPendingMutationsAnalyticsOptions{}
	}

	span := createSpan(am.tracer, opts.ParentSpan, "manager_analytics_get_pending_mutations", "management")
	span.SetAttribute("db.operation", "GET /analytics/node/agg/stats/remaining")
	defer span.End()

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = am.globalTimeout
	}

	req := mgmtRequest{
		Service:       ServiceTypeAnalytics,
		Method:        "GET",
		Path:          "/analytics/node/agg/stats/remaining",
		IsIdempotent:  true,
		RetryStrategy: opts.RetryStrategy,
		Timeout:       timeout,
		parentSpanCtx: span.Context(),
	}
	resp, err := am.doMgmtRequest(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, makeMgmtBadStatusError("failed to get pending mutations", &req, resp)
	}

	pending := make(map[string]map[string]int)
	jsonDec := json.NewDecoder(resp.Body)
	err = jsonDec.Decode(&pending)
	if err != nil {
		return nil, err
	}

	err = resp.Body.Close()
	if err != nil {
		logDebugf("Failed to close socket (%s)", err)
	}

	return pending, nil
}
