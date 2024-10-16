package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"encoding/json"
	"io/ioutil"
	"anumventures.com/wfa/job"
)

func sendFileAndParams(filename string, url string, params job.JobParams) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("could not open file: %w", err)
	}

	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(filename))

	_, err = io.Copy(part, file)
	if err != nil {
		return fmt.Errorf("could not copy file: %w", err)
	}

	b, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON data: %v", err)
	}

	writer.WriteField("params", string(b))
	err = writer.Close()
	if err != nil {
		return fmt.Errorf("failed to close writer: %v", err)
	}

	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}

	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	fmt.Println("Server response:", string(respBody))
	return nil
}

func readParamFile(param_file_path string) job.JobParams {
	param_file, err := os.Open(param_file_path)
	if err != nil {
		fmt.Println(err)
	}

	defer param_file.Close()
	param_bytes, _ := ioutil.ReadAll(param_file)
	var params job.JobParams
	json.Unmarshal(param_bytes, &params)

	return params
}

func main() {
	// Get video input
	filePtr := flag.String("file", "", "the input video file")
	urlPtr := flag.String("url", "", "server URL")
	paramFilePtr := flag.String("params", "", "An input json string that contains the WFA params")
	flag.Parse()

	if *filePtr == "" {
		fmt.Println("Please provide an input file")
		os.Exit(1)
	} 

	input_file := *filePtr

	// Get request URL
	var param_file string
	if *paramFilePtr == "" {
		param_file = "../job/params.json"
		fmt.Println("WFA param file not found, use default params from: ", param_file)
	} else {
		param_file = *paramFilePtr
	}

	// Get server URL
	if *urlPtr == "" {
		fmt.Println("Please provide server URL")
		os.Exit(1)
	} 

	url := *urlPtr

	// Read WFA params from file
	params := readParamFile(param_file)
	err := sendFileAndParams(input_file, url, params)
	if err != nil {
		fmt.Println("Error sending input file and WFA params:", err)
	} else {
		fmt.Println("Input file and WFA params uploaded successfully!")
	}
}