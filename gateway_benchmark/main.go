package main

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperledger/fabric-gateway/pkg/client"
	"github.com/hyperledger/fabric-gateway/pkg/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	mspID        = "Org1MSP"
	peerEndpoint = "localhost:7051"
	gatewayPeer  = "peer0.org1.example.com"
	channelName  = "vqchannel"
	chaincode    = "vqstate"
)

var (
	testNetwork = "/root/fabric-samples/test-network"
	rootText    = fixedBytes("root", 32)
	pcText      = fixedBytes("prefix-commitment", 48)
	sigText     = fixedBytes("signature", 48)
)

type samples struct {
	mu     sync.Mutex
	values []float64
}

func (s *samples) add(value float64) {
	s.mu.Lock()
	s.values = append(s.values, value)
	s.mu.Unlock()
}

type concurrencyRow struct {
	Concurrency int
	PublishMS   float64
	ReadMS      float64
	Publishes   int
	Reads       int
}

type growthRow struct {
	States    int
	PublishMS float64
	ReadMS    float64
	Publishes int
	Reads     int
}

func fixedBytes(tag string, size int) string {
	out := make([]byte, 0, size)
	for counter := 0; len(out) < size; counter++ {
		digest := sha256.Sum256([]byte(fmt.Sprintf("VQStream/%s/%d", tag, counter)))
		out = append(out, digest[:]...)
	}
	return base64.StdEncoding.EncodeToString(out[:size])
}

func mean(values []float64) float64 {
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func timed(function func() error) (float64, error) {
	started := time.Now()
	err := function()
	return float64(time.Since(started).Nanoseconds()) / 1e6, err
}

func main() {
	outDir := flag.String("out-dir", "results", "output directory")
	ops := flag.Int("ops", 50, "operations per worker and round")
	rounds := flag.Int("rounds", 5, "rounds per concurrency level")
	mode := flag.String("mode", "all", "benchmark mode: all, growth, or concurrency")
	smoke := flag.Bool("smoke", false, "run one Publish and Latest operation")
	flag.Parse()

	connection := grpcConnection()
	defer connection.Close()
	gateway, err := client.Connect(
		newIdentity(), client.WithSign(newSign()),
		client.WithClientConnection(connection),
		client.WithEvaluateTimeout(30*time.Second),
		client.WithEndorseTimeout(30*time.Second),
		client.WithSubmitTimeout(30*time.Second),
		client.WithCommitStatusTimeout(2*time.Minute),
	)
	check(err)
	defer gateway.Close()
	contract := gateway.GetNetwork(channelName).GetContract(chaincode)

	check(os.MkdirAll(*outDir, 0o755))
	seedAnchor(contract)
	if *smoke {
		anchor := os.Getenv("VQ_READ_ANCHOR")
		result, err := contract.EvaluateTransaction("Latest", anchor)
		check(err)
		fmt.Printf("smoke latest: %s\n", result)
		return
	}
	if *mode != "all" && *mode != "growth" && *mode != "concurrency" {
		panic("mode must be all, growth, or concurrency")
	}
	if *mode == "all" || *mode == "growth" {
		growthPath := filepath.Join(*outDir, "ledger_growth.csv")
		growth := runGrowth(contract, growthPath)
		writeGrowth(growthPath, growth)
	}
	if *mode == "all" || *mode == "concurrency" {
		concurrencyPath := filepath.Join(*outDir, "concurrency.csv")
		concurrency := runConcurrency(contract, *ops, *rounds, concurrencyPath)
		writeConcurrency(concurrencyPath, concurrency)
	}
	writeManifest(filepath.Join(*outDir, "manifest.json"), *ops, *rounds)
}

func seedAnchor(contract *client.Contract) {
	stream := fmt.Sprintf("read-anchor-%d", time.Now().UnixNano())
	_, err := contract.SubmitTransaction("Publish", stream, "1", rootText, pcText, sigText)
	check(err)
	os.Setenv("VQ_READ_ANCHOR", stream)
}

func runGrowth(contract *client.Contract, checkpointPath string) []growthRow {
	targets := []int{32, 64, 128, 256, 512, 1024, 2048, 4096}
	const prefillWorkers = 16
	prefix := fmt.Sprintf("growth-%d", time.Now().UnixNano())
	prefillStreams := make([]string, prefillWorkers)
	prefillWindows := make([]int, prefillWorkers)
	for worker := range prefillStreams {
		prefillStreams[worker] = fmt.Sprintf("%s-fill-%d", prefix, worker)
	}
	measureStream := prefix + "-measure"
	measureWindow := 0
	published := 0
	rows := make([]growthRow, 0, len(targets))
	for _, target := range targets {
		if published < target {
			fillPublishedStates(contract, prefillStreams, prefillWindows, target-published)
			published = target
		}
		publish := make([]float64, 0, 20)
		for index := 0; index < 20; index++ {
			measureWindow++
			window := strconv.Itoa(measureWindow)
			latency, err := timed(func() error {
				_, err := contract.SubmitTransaction("Publish", measureStream, window, rootText, pcText, sigText)
				return err
			})
			check(err)
			publish = append(publish, latency)
			published++
		}
		reads := make([]float64, 0, 100)
		for index := 0; index < 100; index++ {
			latency, err := timed(func() error {
				_, err := contract.EvaluateTransaction("Latest", measureStream)
				return err
			})
			check(err)
			reads = append(reads, latency)
		}
		rows = append(rows, growthRow{target, mean(publish), mean(reads), len(publish), len(reads)})
		writeGrowth(checkpointPath, rows)
		fmt.Printf("growth L=%d publish=%.3f read=%.3f\n", target, mean(publish), mean(reads))
	}
	return rows
}

func fillPublishedStates(contract *client.Contract, streams []string, windows []int, count int) {
	var next atomic.Int64
	var wait sync.WaitGroup
	for worker := range streams {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for {
				job := int(next.Add(1)) - 1
				if job >= count {
					return
				}
				windows[worker]++
				_, err := contract.SubmitTransaction(
					"Publish", streams[worker], strconv.Itoa(windows[worker]),
					rootText, pcText, sigText,
				)
				check(err)
			}
		}(worker)
	}
	wait.Wait()
}

func runConcurrency(contract *client.Contract, operations, rounds int, checkpointPath string) []concurrencyRow {
	levels := []int{1, 2, 4, 8, 16, 24, 32, 40}
	anchor := os.Getenv("VQ_READ_ANCHOR")
	rows := make([]concurrencyRow, 0, len(levels))
	for _, level := range levels {
		publish := &samples{}
		reads := &samples{}
		for round := 0; round < rounds; round++ {
			var wait sync.WaitGroup
			start := make(chan struct{})
			for worker := 0; worker < level; worker++ {
				wait.Add(1)
				go func(round, worker int) {
					defer wait.Done()
					stream := fmt.Sprintf("conc-%d-c%d-r%d-w%d", time.Now().UnixNano(), level, round, worker)
					<-start
					for operation := 1; operation <= operations; operation++ {
						window := strconv.Itoa(operation)
						latency, err := timed(func() error {
							_, err := contract.SubmitTransaction("Publish", stream, window, rootText, pcText, sigText)
							return err
						})
						check(err)
						publish.add(latency)
						latency, err = timed(func() error {
							_, err := contract.EvaluateTransaction("Latest", anchor)
							return err
						})
						check(err)
						reads.add(latency)
					}
				}(round, worker)
			}
			close(start)
			wait.Wait()
		}
		rows = append(rows, concurrencyRow{level, mean(publish.values), mean(reads.values), len(publish.values), len(reads.values)})
		writeConcurrency(checkpointPath, rows)
		fmt.Printf("concurrency c=%d publish=%.3f read=%.3f samples=%d\n", level, mean(publish.values), mean(reads.values), len(publish.values))
	}
	return rows
}

func grpcConnection() *grpc.ClientConn {
	tlsPath := filepath.Join(testNetwork, "organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt")
	certificate := loadCertificate(tlsPath)
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	connection, err := grpc.Dial(peerEndpoint, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(pool, gatewayPeer)))
	check(err)
	return connection
}

func newIdentity() *identity.X509Identity {
	certDir := filepath.Join(testNetwork, "organizations/peerOrganizations/org1.example.com/users/User1@org1.example.com/msp/signcerts")
	files, err := os.ReadDir(certDir)
	check(err)
	if len(files) == 0 {
		panic("no Org1 user signing certificate")
	}
	id, err := identity.NewX509Identity(mspID, loadCertificate(filepath.Join(certDir, files[0].Name())))
	check(err)
	return id
}

func newSign() identity.Sign {
	keyDir := filepath.Join(testNetwork, "organizations/peerOrganizations/org1.example.com/users/User1@org1.example.com/msp/keystore")
	files, err := os.ReadDir(keyDir)
	check(err)
	privateKeyPEM, err := os.ReadFile(filepath.Join(keyDir, files[0].Name()))
	check(err)
	privateKey, err := identity.PrivateKeyFromPEM(privateKeyPEM)
	check(err)
	sign, err := identity.NewPrivateKeySign(privateKey)
	check(err)
	return sign
}

func loadCertificate(path string) *x509.Certificate {
	pem, err := os.ReadFile(path)
	check(err)
	certificate, err := identity.CertificateFromPEM(pem)
	check(err)
	return certificate
}

func writeConcurrency(path string, rows []concurrencyRow) {
	file, err := os.Create(path)
	check(err)
	defer file.Close()
	writer := csv.NewWriter(file)
	check(writer.Write([]string{"concurrency", "publication_ms_mean", "read_ms_mean", "publication_samples", "read_samples"}))
	for _, row := range rows {
		check(writer.Write([]string{strconv.Itoa(row.Concurrency), fmt.Sprintf("%.3f", row.PublishMS), fmt.Sprintf("%.3f", row.ReadMS), strconv.Itoa(row.Publishes), strconv.Itoa(row.Reads)}))
	}
	writer.Flush()
	check(writer.Error())
}

func writeGrowth(path string, rows []growthRow) {
	file, err := os.Create(path)
	check(err)
	defer file.Close()
	writer := csv.NewWriter(file)
	check(writer.Write([]string{"published_states", "publication_ms_mean", "read_ms_mean", "publication_samples", "read_samples"}))
	for _, row := range rows {
		check(writer.Write([]string{strconv.Itoa(row.States), fmt.Sprintf("%.3f", row.PublishMS), fmt.Sprintf("%.3f", row.ReadMS), strconv.Itoa(row.Publishes), strconv.Itoa(row.Reads)}))
	}
	writer.Flush()
	check(writer.Error())
}

func writeManifest(path string, operations, rounds int) {
	manifest := map[string]any{
		"created_utc": time.Now().UTC().Format(time.RFC3339Nano),
		"channel":     channelName, "chaincode": chaincode,
		"concurrency_operations_per_worker_round": operations,
		"concurrency_rounds":                      rounds,
		"growth_publication_samples":              20, "growth_read_samples": 100,
		"growth_prefill": "one committed Publish transaction per state using 16 independent streams",
		"root_bytes":     32, "prefix_commitment_bytes": 48, "signature_bytes": 48,
		"publication_timing": "Gateway SubmitTransaction through successful commit",
		"read_timing":        "Gateway EvaluateTransaction through complete response",
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	check(err)
	check(os.WriteFile(path, append(raw, '\n'), 0o644))
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
