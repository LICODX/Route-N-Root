// main.go (untuk genesis-tool)
package main

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/scrypt"
	"golang.org/x/term"
)

// KeystoreFile merepresentasikan struktur file JSON dompet yang terenkripsi.
// Ini adalah format yang aman untuk menyimpan kunci privat.
type KeystoreFile struct {
	Address string      `json:"address"`
	Crypto  cryptoJSON  `json:"crypto"`
	Version int         `json:"version"`
}

type cryptoJSON struct {
	Cipher       string       `json:"cipher"`
	CipherText   string       `json:"ciphertext"`
	CipherParams cipherParams `json:"cipherparams"`
	KDF          string       `json:"kdf"`
	KDFParams    kdfParams    `json:"kdfparams"`
	MAC          string       `json:"mac"`
}

type cipherParams struct {
	IV string `json:"iv"`
}

type kdfParams struct {
	DKLen int    `json:"dklen"`
	N     int    `json:"n"`
	P     int    `json:"p"`
	R     int    `json:"r"`
	Salt  string `json:"salt"`
}

// GenesisConfig adalah struktur untuk file genesis.json.
type GenesisConfig struct {
	ChainID           string                   `json:"chainId"`
	NetworkName       string                   `json:"networkName"`
	InitialValidators map[string]InitialStake `json:"initialValidators"`
}

type InitialStake struct {
	Stake string `json:"stake"`
}

func main() {
	// --- Konfigurasi Flag Command-Line ---
	validatorsCount := flag.Int("validators", 1, "Jumlah validator genesis yang akan dibuat.")
	chainID := flag.String("chainId", "rnr-mainnet-v1", "ID unik untuk jaringan blockchain.")
	networkName := flag.String("networkName", "RNR Mainnet", "Nama jaringan.")
	outputDir := flag.String("outputDir", "./genesis-data", "Direktori untuk menyimpan file genesis dan keystore.")
	flag.Parse()

	fmt.Println("🚀 RNR Genesis Tool - Secure Wallet Generator 🚀")
	fmt.Println("-------------------------------------------------")

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("❌ Gagal membuat direktori output: %v", err)
	}

	genesisConfig := &GenesisConfig{
		ChainID:           *chainID,
		NetworkName:       *networkName,
		InitialValidators: make(map[string]InitialStake),
	}

	// --- Loop untuk Membuat Setiap Validator ---
	for i := 0; i < *validatorsCount; i++ {
		fmt.Printf("\n--- 👤 Membuat Validator #%d ---\n", i+1)

		// 1. Buat Mnemonic dan Kunci Privat
		mnemonic, privateKey, err := generateKeys()
		if err != nil {
			log.Fatalf("❌ Gagal membuat kunci untuk validator #%d: %v", i+1, err)
		}

		// 2. Tampilkan Mnemonic (SANGAT PENTING!)
		fmt.Println("🔥 PENTING! Simpan Mnemonic Phrase ini di tempat yang sangat aman (offline).")
		fmt.Println("🔥 Ini adalah satu-satunya cara untuk memulihkan dompet Anda jika file atau kata sandi hilang.")
		fmt.Println("-------------------------------------------------------------------------------------")
		fmt.Printf("🔐 Mnemonic: %s\n", mnemonic)
		fmt.Println("-------------------------------------------------------------------------------------")
		fmt.Print("Tekan 'Enter' setelah Anda mencatat mnemonic ini dengan aman...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')

		// 3. Dapatkan Kata Sandi yang Kuat dari Pengguna
		password, err := getPassword()
		if err != nil {
			log.Fatalf("❌ Gagal mendapatkan kata sandi: %v", err)
		}

		// 4. Enkripsi Kunci Privat dan Simpan sebagai File Keystore
		publicKey := privateKey.Public()
		publicKeyECDSA, _ := publicKey.(*ecdsa.PublicKey)
		address := crypto.PubkeyToAddress(*publicKeyECDSA).Hex()

		keystoreFileName := fmt.Sprintf("wallet-%s.json", address)
		keystoreFilePath := filepath.Join(*outputDir, keystoreFileName)

		if err := createKeystoreFile(keystoreFilePath, privateKey, password); err != nil {
			log.Fatalf("❌ Gagal membuat file keystore: %v", err)
		}

		fmt.Printf("✅ Berhasil membuat dompet terenkripsi untuk alamat: %s\n", address)
		fmt.Printf("   📄 Disimpan di: %s\n", keystoreFilePath)

		// 5. Tambahkan Validator ke Konfigurasi Genesis
		// Alokasi awal 1000 RNR (dengan 18 desimal)
		genesisConfig.InitialValidators[address] = InitialStake{Stake: "100"}
	}

	// --- Tulis File genesis.json ---
	genesisFilePath := filepath.Join(*outputDir, "genesis.json")
	genesisFile, err := os.Create(genesisFilePath)
	if err != nil {
		log.Fatalf("❌ Gagal membuat file genesis.json: %v", err)
	}
	defer genesisFile.Close()

	encoder := json.NewEncoder(genesisFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(genesisConfig); err != nil {
		log.Fatalf("❌ Gagal menulis ke file genesis.json: %v", err)
	}

	fmt.Println("\n-------------------------------------------------")
	fmt.Println("🎉 SELAMAT! Proses pembuatan genesis telah selesai.")
	fmt.Printf("   📄 File Genesis: %s\n", genesisFilePath)
	fmt.Printf("   🔐 File Dompet: %s/\n", *outputDir)
	fmt.Println("   ⚠️  AMANKAN direktori ini dan jangan pernah membagikan kata sandi atau mnemonic Anda!")
}

// generateKeys membuat mnemonic 24 kata dan kunci privat ECDSA P256.
func generateKeys() (string, *ecdsa.PrivateKey, error) {
	entropy, err := bip39.NewEntropy(256)
	if err != nil {
		return "", nil, err
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", nil, err
	}
	seed := bip39.NewSeed(mnemonic, "") // Kata sandi kosong untuk seed
	privateKey, err := crypto.ToECDSA(seed[:32])
	if err != nil {
		return "", nil, err
	}
	return mnemonic, privateKey, nil
}

// getPassword meminta pengguna untuk memasukkan dan mengonfirmasi kata sandi secara aman.
func getPassword() (string, error) {
	fmt.Print("🔑 Masukkan kata sandi yang kuat untuk mengenkripsi dompet: ")
	bytePassword, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	password := string(bytePassword)
	fmt.Println() // Pindah ke baris baru

	fmt.Print("🔑 Konfirmasi kata sandi Anda: ")
	bytePasswordConfirm, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	passwordConfirm := string(bytePasswordConfirm)
	fmt.Println()

	if password != passwordConfirm {
		return "", fmt.Errorf("kata sandi tidak cocok")
	}
	if len(password) < 8 {
		fmt.Println("⚠️  PERINGATAN: Kata sandi Anda kurang dari 8 karakter. Disarankan menggunakan kata sandi yang lebih panjang.")
	}
	return password, nil
}

// createKeystoreFile mengenkripsi kunci privat dan menyimpannya ke file.
func createKeystoreFile(path string, privateKey *ecdsa.PrivateKey, password string) error {
	// Standar parameter Scrypt
	N, r, p, dkLen := 1<<18, 8, 1, 32

	salt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}

	derivedKey, err := scrypt.Key([]byte(password), salt, N, r, p, dkLen)
	if err != nil {
		return err
	}

	iv := make([]byte, 16) // AES-128-CTR membutuhkan IV 16-byte
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return err
	}

	privateKeyBytes := crypto.FromECDSA(privateKey)
	block, err := aes.NewCipher(derivedKey[:16])
	if err != nil {
		return err
	}

	stream := cipher.NewCTR(block, iv)
	cipherText := make([]byte, len(privateKeyBytes))
	stream.XORKeyStream(cipherText, privateKeyBytes)

	mac := crypto.Keccak256(derivedKey[16:32], cipherText)

	keystore := &KeystoreFile{
		Address: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Crypto: cryptoJSON{
			Cipher:     "aes-128-ctr",
			CipherText: hex.EncodeToString(cipherText),
			CipherParams: cipherParams{
				IV: hex.EncodeToString(iv),
			},
			KDF: "scrypt",
			KDFParams: kdfParams{
				N:     N,
				R:     r,
				P:     p,
				DKLen: dkLen,
				Salt:  hex.EncodeToString(salt),
			},
			MAC: hex.EncodeToString(mac),
		},
		Version: 3,
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(keystore)
}
