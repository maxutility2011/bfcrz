package job

import (
	//"encoding/json"
	//"strconv"
	"strings"
)

func ArgumentArrayToString(args []string) string {
	return strings.Join(args, " ")
}

func Get_frame_diff_output(input_file string) string {
	return input_file + ".diff"
}

func Get_frame_diff_args(input_file string) ([]string, string) {
	var diff_args []string
	diff_args = append(diff_args, "-i")
	diff_args = append(diff_args, input_file)

	diff_args = append(diff_args, "-vf")
	diff_filter := "tblend=all_mode=difference"
	diff_args = append(diff_args, diff_filter)

	diff_args = append(diff_args, "-f")
	diff_args = append(diff_args, "mp4")

	diff_out := Get_frame_diff_output(input_file)
	diff_args = append(diff_args, diff_out)

	return diff_args, diff_out
}