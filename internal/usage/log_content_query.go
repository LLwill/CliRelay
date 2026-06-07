package usage

import (
	"database/sql"
	"fmt"
)

func queryCompressedLogContent(db *sql.DB, query string, args ...any) (LogContentResult, error) {
	if db == nil {
		return LogContentResult{}, fmt.Errorf("usage: database not initialised")
	}

	var (
		result           LogContentResult
		compression      string
		inputCompressed  []byte
		outputCompressed []byte
	)
	err := db.QueryRow(query, args...).Scan(
		&result.ID,
		&result.Model,
		&compression,
		&inputCompressed,
		&outputCompressed,
	)
	if err != nil {
		return LogContentResult{}, err
	}

	inputContent, err := decompressLogContent(compression, inputCompressed)
	if err != nil {
		return LogContentResult{}, err
	}
	outputContent, err := decompressLogContent(compression, outputCompressed)
	if err != nil {
		return LogContentResult{}, err
	}
	result.InputContent = inputContent
	result.OutputContent = outputContent
	return result, nil
}

func queryCompressedLogContentPart(
	db *sql.DB,
	part string,
	query string,
	args ...any,
) (LogContentPartResult, error) {
	if db == nil {
		return LogContentPartResult{}, fmt.Errorf("usage: database not initialised")
	}

	var (
		result      LogContentPartResult
		compression string
		compressed  []byte
	)
	result.Part = part
	err := db.QueryRow(query, args...).Scan(
		&result.ID,
		&result.Model,
		&compression,
		&compressed,
	)
	if err != nil {
		return LogContentPartResult{}, err
	}

	content, err := decompressLogContent(compression, compressed)
	if err != nil {
		return LogContentPartResult{}, err
	}
	result.Content = content
	return result, nil
}
