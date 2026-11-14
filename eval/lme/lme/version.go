package lme

// The pinned dataset (see eval/lme/DATASET.md).
const (
	DatasetRepo     = "xiaowu0162/longmemeval-cleaned"
	DatasetFile     = "longmemeval_s_cleaned.json"
	DatasetRevision = "98d7416c24c778c2fee6e6f3006e7a073259d48f"
	DatasetSHA256   = "d6f21ea9d60a0d56f34a05b609c79c88a451d2ae03597821ea3d5a9678c3a442"
	DatasetSize     = 277383467
	DatasetURL      = "https://huggingface.co/datasets/" + DatasetRepo + "/resolve/" + DatasetRevision + "/" + DatasetFile
)

// The official LongMemEval code read for the metric definitions.
const (
	OfficialRepo   = "github.com/xiaowu0162/LongMemEval"
	OfficialCommit = "9e0b455f4ef0e2ab8f2e582289761153549043fc"
)
