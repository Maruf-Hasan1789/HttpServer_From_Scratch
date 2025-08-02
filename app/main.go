package main

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Ensures gofmt doesn't remove the "net" and "os" imports above (feel free to remove this!)
var _ = net.Listen
var _ = os.Exit

type HttpRequest struct {
	Method  string
	URL     string
	Version string
	Headers map[string]string
	Body    []byte
}

type HttpResponse struct {
	Status  int
	Version string
	Headers map[string]string
	Body    []byte
}

func buildResponse(status int, version string) *HttpResponse {
	return &HttpResponse{
		Status:  status,
		Version: version,
		Headers: make(map[string]string),
		Body:    []byte(""),
	}
}

var availableEncodingType = []string{"gzip"}

func getGzipCompressedData(body []byte) ([]byte, error) {
	var compressedData bytes.Buffer
	gz := gzip.NewWriter(&compressedData)
	_, err := gz.Write(body)

	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	err = gz.Close()

	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	return compressedData.Bytes(), nil
}

func buildResponseWithBody(status int, version string, body []byte, contentType string, acceptedEncodings []string, connectionState string) *HttpResponse {
	header := make(map[string]string)
	header["Content-Type"] = contentType
	header["Content-Length"] = strconv.Itoa(len(body))
	if connectionState == "close" {
		header["Connection"] = connectionState
	}

	for _, acceptedEncoding := range acceptedEncodings {
		acceptedEncoding = strings.Trim(acceptedEncoding, " ")
		if slices.Contains(availableEncodingType, acceptedEncoding) {
			header["Content-Encoding"] = acceptedEncoding
			compressedBody, err := getGzipCompressedData(body)
			if err != nil {
				fmt.Println(err)
				return nil
			}
			body = compressedBody
			header["Content-Length"] = strconv.Itoa(len(body))
			break
		}
	}

	return &HttpResponse{
		Status:  status,
		Version: version,
		Headers: header,
		Body:    body,
	}
}

var fileDirectory *string

func main() {

	// You can use print statements as follows for debugging, they'll be visible when running tests.
	fmt.Println("Logs from your program will appear here!")

	fileDirectory = flag.String("directory", "/home/marufhasan/Documents", "File Directory")
	flag.Parse()

	l, err := net.Listen("tcp", "0.0.0.0:4221")
	if err != nil {
		fmt.Println("Failed to bind to port 8080")
		os.Exit(1)
	}

	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: ", err.Error())
			os.Exit(1)
		}

		go handleClient(conn)
	}
}

func handleClient(conn net.Conn) {
	defer conn.Close()
	for {
		buf := make([]byte, 1024)

		n, err := conn.Read(buf)
		if err != nil {
			log.Println(err)
			return
		}

		httpRequest := parseRequest(buf[:n])

		response := route(httpRequest)

		encodeResponse := encodeResponse(response)

		sendResponse(conn, encodeResponse)

		if httpRequest.Headers["Connection"] == "close" {
			return
		}
	}
}

func sendResponse(conn net.Conn, encodeResponse []byte) {
	conn.Write(encodeResponse)
}

func encodeResponse(response *HttpResponse) []byte {
	result := make([]byte, 0)
	result = append(result, response.Version...)
	result = append(result, ' ')
	result = append(result, buildStatus(response.Status)...)
	result = append(result, "\r\n"...)
	fmt.Println("Here")
	for key, value := range response.Headers {
		result = append(result, key...)
		result = append(result, ": "...)
		result = append(result, value...)
		result = append(result, "\r\n"...)
	}

	result = append(result, "\r\n"...)
	result = append(result, response.Body...)
	return result
}

func buildStatus(status int) string {
	switch status {
	case 200:
		return "200 OK"
	case 404:
		return "404 Not Found"
	case 201:
		return "201 Created"
	default:
		return ""
	}
}

func getFileByName(fileName string) ([]byte, error) {

	entries, err := os.ReadDir(*fileDirectory)
	if err != nil {
		fmt.Println("Error : ", err)
		return nil, err
	}

	fileMap := make(map[string]string)

	for _, entry := range entries {
		if !entry.IsDir() {
			fileBaseName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			fileMap[fileBaseName] = filepath.Join(*fileDirectory, entry.Name())
		}
	}

	if fileLocation, ok := fileMap[fileName]; ok == true {
		fileContent, err := os.ReadFile(fileLocation)
		if err != nil {
			fmt.Println(err)
			return nil, err
		}

		return fileContent, nil
	}

	return nil, nil
}

func route(request *HttpRequest) *HttpResponse {
	if request.URL == "/" {
		return buildResponse(200, request.Version)
	} else if strings.HasPrefix(request.URL, "/echo") {
		path := request.URL[6:]
		acceptedEncoding := strings.Split(request.Headers["Accept-Encoding"], ",")
		httpResponse := buildResponseWithBody(200, request.Version, []byte(path), "text/plain", acceptedEncoding, request.Headers["Connection"])
		return httpResponse
	} else if strings.HasPrefix(request.URL, "/user-agent") {
		return buildResponseWithBody(200, request.Version, []byte(request.Headers["User-Agent"]), "text/plain", nil, request.Headers["Connection"])
	} else if strings.HasPrefix(request.URL, "/files/") && request.Method == "POST" {
		newFileName := strings.TrimPrefix(request.URL, "/files/")
		file, err := os.Create(filepath.Join(*fileDirectory, newFileName))
		if err != nil {
			fmt.Println(err)
			return buildResponse(404, request.Version)
		}

		_, err = file.Write(request.Body)

		if err != nil {
			fmt.Println(err)
			return buildResponse(404, request.Version)
		}

		return buildResponse(201, request.Version)

	} else if strings.HasPrefix(request.URL, "/files/") {
		requestFileName := strings.TrimPrefix(request.URL, "/files/")
		fileContent, err := getFileByName(requestFileName)

		if len(fileContent) == 0 || err != nil {
			return buildResponse(404, request.Version)
		}

		return buildResponseWithBody(200, request.Version, fileContent, "application/octet-stream", nil, request.Headers["Connection"])
	} else {
		return buildResponse(404, request.Version)
	}
}

func parseRequest(b []byte) *HttpRequest {

	lines := splitLines(b, "\r\n\r\n")
	requestLinesWithHeaders := splitLines(lines[0], "\r\n")
	requestLines := splitLines(requestLinesWithHeaders[0], " ")

	method := requestLines[0]
	url := requestLines[1]
	httpVersion := requestLines[2]
	headersMap := createHeadersMap(requestLinesWithHeaders[1:])

	if len(lines) == 1 {
		return &HttpRequest{
			Method:  string(method),
			URL:     string(url),
			Version: string(httpVersion),
			Headers: headersMap,
		}
	}

	return &HttpRequest{
		Method:  string(method),
		URL:     string(url),
		Version: string(httpVersion),
		Headers: headersMap,
		Body:    lines[1],
	}
}

func createHeadersMap(b [][]byte) map[string]string {
	headersMap := make(map[string]string)
	for i := range b {
		header := splitLines(b[i], ": ")
		if len(header) < 2 {
			break
		}
		headersMap[string(header[0])] = string(header[1])
	}

	return headersMap
}

func splitLines(b []byte, separator string) [][]byte {
	s := make([][]byte, 0)
	currentIndex := 0

	for i := 0; i < len(b)-len(separator); i++ {
		if string(b[i:i+len(separator)]) == separator {
			s = append(s, b[currentIndex:i])
			currentIndex = i + len(separator)
		}
	}

	if currentIndex != len(b) {
		s = append(s, b[currentIndex:])
	}

	return s
}
