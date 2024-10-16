package main

import (
	"flag"
	"fmt"
	"time"
	"errors"
	"io/ioutil"
	"encoding/json"
	"log/slog" // requires golang 1.22+
	"net/http"
	"os"
	"io"
	"os/exec"
	"anumventures.com/wfa/job"
)

type ServerConfig struct {
	Hostname string
	Port string
	Loglevel string
	File_storage string
}

type AppLogLevel struct { 
	Loglevel string
}

func readConfig(config_file_path string) ServerConfig {
	configFile, err := os.Open(config_file_path)
	if err != nil {
		slog.Error(err.Error())
	}

	defer configFile.Close()
	config_bytes, _ := ioutil.ReadAll(configFile)
	var config ServerConfig
	json.Unmarshal(config_bytes, &config)
	
	if config.Hostname == "" {
		config.Hostname = "0.0.0.0"
	}

	if config.Port == "" {
		config.Port = "5001"
	}

	if config.Loglevel == "" {
		config.Loglevel = "error"
	}

	return config
}

func contains_string(a []string, v string) bool {
	r := false
	for _, e := range a {
		if v == e {
			r = true
		}
	}

	return r
}

var valid_log_levels = []string{"debug", "info", "warn", "error"}
const MaxFileSizeMb = 50000 // 50GB
var server_config ServerConfig
var app_log_level slog.LevelVar

func loglevel_handler(w http.ResponseWriter, r *http.Request) {
	slog.Info("----------------------------------------")
	slog.Info("Received new request to change log level:")
	slog.Info(r.Method, r.URL.Path)

	if !(r.Method == "GET" || r.Method == "PUT") {
		err := "Method = " + r.Method + " is not allowed to " + r.URL.Path
		slog.Error(err)
		http.Error(w, "405 method not allowed\n  Error: "+err, http.StatusMethodNotAllowed)
		return
	}

	if r.Method == "GET" {
		var resp AppLogLevel
		if app_log_level.Level() == slog.LevelDebug {
			resp.Loglevel = "debug"
		} else if app_log_level.Level() == slog.LevelInfo {
			resp.Loglevel = "info"
		} else if app_log_level.Level() == slog.LevelWarn {
			resp.Loglevel = "warn"
		} else if app_log_level.Level() == slog.LevelError {
			resp.Loglevel = "error"
		}

		FileContentType := "application/json"
		w.Header().Set("Content-Type", FileContentType)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
		return
	} else if r.Method == "PUT" {
		var l AppLogLevel
		err := json.NewDecoder(r.Body).Decode(&l)
		if err != nil {
			res := "Failed to decode create change_log_level request"
			slog.Error(res)
			http.Error(w, "400 bad request\n  Error: "+res, http.StatusBadRequest)
			return
		}

		if !contains_string(valid_log_levels, l.Loglevel) {
			slog.Error("Invalid log level")
			res := "Invalid log level: " + l.Loglevel
			slog.Error(res)
			http.Error(w, "400 bad request\n  Error: "+res, http.StatusBadRequest)
			return
		} else {
			app_log_level.Set(slog.LevelWarn)
			w.WriteHeader(http.StatusOK)
		}
	}
}

func run_frame_diff(input_file string) (error, string) {
	frame_diff_args, diff_output := job.Get_frame_diff_args(input_file)
	slog.Info("Frame diff command:")
	slog.Info(job.ArgumentArrayToString(frame_diff_args))

	frame_diff_cmd := exec.Command("ffmpeg", frame_diff_args...)
	var err error
	var out []byte
	go func() {
		out, err = frame_diff_cmd.CombinedOutput() // This line blocks after frame_diff_cmd launches
		if err != nil {
			slog.Error("Errors starting frame diff command: ", err, ". Command output: ", string(out))
			// os.Exit(1) // Do not exit worker_transcoder here since ffmpeg also needs to be stopped after the packager is stopped. Let function manageCommand() to handle this.
		}
	}()

	time.Sleep(100 * time.Millisecond)
	if err != nil {
		slog.Error("Error: ", string(out))
		return errors.New(string(out)), diff_output
		//os.Exit(1)
	}

	return err, diff_output
}

func run_authentication(input_file string, params job.JobParams) error {
	err_diff, diff_output := run_frame_diff(input_file)
	if err_diff != nil {
		slog.Error("Errors starting frame diff command. Error message:", err_diff)
		return errors.New("fail_to_authenticate_input")
	}

	_, err_stat := os.Stat(diff_output)
	if errors.Is(err_stat, os.ErrNotExist) {
		return errors.New("fail_to_create_frame_diff_output")
	}

	return nil
}

func upload_handler(w http.ResponseWriter, r *http.Request) {
	slog.Info("----------------------------------------")
	slog.Info("Received new request to upload video:")
	slog.Info(r.Method, r.URL.Path)

	if !(r.Method == "POST") {
		err := "Method = " + r.Method + " is not allowed to " + r.URL.Path
		slog.Error(err)
		http.Error(w, "405 method not allowed\n  Error: "+err, http.StatusMethodNotAllowed)
		return
	}

	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, MaxFileSizeMb << 20) // Uploaded video file size limit: 500 MB
		err := r.ParseMultipartForm(MaxFileSizeMb << 20) // 50 GB limit for file parsing
		if err != nil {
			fmt.Println(err)
			http.Error(w, "File too large.", http.StatusBadRequest)
			slog.Error("File too large.")
			return
		}

		file, handler, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Error retrieving the file.", http.StatusBadRequest)
			slog.Error("Error retrieving the file.")
			return
		}

		defer file.Close()
		dst, err := os.Create(handler.Filename)
		if err != nil {
			http.Error(w, "Unable to save the file.", http.StatusInternalServerError)
			slog.Error("Unable to save the file.")
			return
		}

		defer dst.Close()
		_, err = io.Copy(dst, file)
		if err != nil {
			http.Error(w, "Failed to save the file.", http.StatusInternalServerError)
			slog.Error("Failed to save the file.")
			return
		}

		params_string := r.FormValue("params")
		slog.Info("Params:", params_string)
		var params job.JobParams
		//e := json.NewDecoder([]byte(params_string)).Decode(&params)
		e := json.Unmarshal([]byte(params_string), &params)
		if e != nil {
			res := "Failed to decode job params"
			slog.Error("Error happened in JSON marshal. Err: ", e)
			http.Error(w, "400 bad request\n  Error: "+res, http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusAccepted)

		slog.Info("Params:", params)
		err = run_authentication(handler.Filename, params)
		if err != nil {
			res := "Failed to authenticate input video."
			slog.Error("Failed to authenticate input video. Error: ", err)
			http.Error(w, "500 internal server error\n  Error: "+res, http.StatusInternalServerError)
			return
		}
	}

	/*
	e1, j := createJob(jspec, warnings)
	if e1 != nil {
		http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
		return
	}
		*/
}

func main() {
	configPtr := flag.String("config", "", "config file path")
	flag.Parse()

	var config_file_path string
	if *configPtr != "" {
		config_file_path = *configPtr
	} else {
		config_file_path = "config.json"
	}

	server_config = readConfig(config_file_path)
	if server_config.Loglevel == "" {
		app_log_level.Set(slog.LevelError)
	} else if server_config.Loglevel == "debug" {
		app_log_level.Set(slog.LevelDebug)
	} else if server_config.Loglevel == "info" {
		app_log_level.Set(slog.LevelInfo)
	} else if server_config.Loglevel == "warn" {
		app_log_level.Set(slog.LevelWarn)
	} else if server_config.Loglevel == "error" {
		app_log_level.Set(slog.LevelError)
	} else {
		fmt.Printf("Unknown log level: %s, use the least verbose level: error. Valid levels are: debug, info, warn and error (ordered in decreasing verbosity).\n", server_config.Loglevel)
		app_log_level.Set(slog.LevelError)
	}

	logfile, err := os.Create("server.log")
	if err != nil {
    	panic(err)
	}

	h := slog.NewTextHandler(logfile, &slog.HandlerOptions{Level: &app_log_level})
	slog.SetDefault(slog.New(h))

	server_addr := server_config.Hostname + ":" + server_config.Port
	
	fmt.Printf("API server listening on: %s\n", server_addr)
	http.HandleFunc("/loglevel", loglevel_handler)
	http.HandleFunc("/upload", upload_handler)
	http.ListenAndServe(server_addr, nil)
}