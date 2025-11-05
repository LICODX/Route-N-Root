package wallet

import (
        "crypto/aes"
        "crypto/cipher"
        "crypto/ecdsa"
        "crypto/elliptic"
        "crypto/rand"
        "crypto/sha256"
        "encoding/hex"
        "fmt"
        "io"
        "math/big"
        "time"

        "github.com/tyler-smith/go-bip32"
        "github.com/tyler-smith/go-bip39"
        "golang.org/x/crypto/scrypt"
        "rnr-blockchain/pkg/core"
)

type Wallet struct {
        PrivateKey *ecdsa.PrivateKey
        PublicKey  *ecdsa.PublicKey
        Address    string
}

func NewWalletFromMnemonic(mnemonic string) (*Wallet, error) {
        if !bip39.IsMnemonicValid(mnemonic) {
                return nil, fmt.Errorf("invalid mnemonic phrase")
        }

        seed := bip39.NewSeed(mnemonic, "")
        masterKey, err := bip32.NewMasterKey(seed)
        if err != nil {
                return nil, fmt.Errorf("failed to create master key: %w", err)
        }

        childKey, err := masterKey.NewChildKey(bip32.FirstHardenedChild + 44)
        if err != nil {
                return nil, fmt.Errorf("failed to derive child key: %w", err)
        }

        privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
        if err != nil {
                return nil, fmt.Errorf("failed to generate key: %w", err)
        }

        privateKey.D = new(big.Int).SetBytes(childKey.Key)
        privateKey.PublicKey.X, privateKey.PublicKey.Y = privateKey.PublicKey.Curve.ScalarBaseMult(childKey.Key)

        address, err := GenerateAddress(privateKey.PublicKey)
        if err != nil {
                return nil, fmt.Errorf("failed to generate address: %w", err)
        }

        return &Wallet{
                PrivateKey: privateKey,
                PublicKey:  &privateKey.PublicKey,
                Address:    address,
        }, nil
}

func GenerateMnemonic() (string, error) {
        entropy, err := bip39.NewEntropy(128)
        if err != nil {
                return "", fmt.Errorf("failed to generate entropy: %w", err)
        }
        return bip39.NewMnemonic(entropy)
}

func GenerateAddress(pubKey ecdsa.PublicKey) (string, error) {
        pubKeyBytes := elliptic.Marshal(pubKey.Curve, pubKey.X, pubKey.Y)
        hash := sha256.Sum256(pubKeyBytes)
        address := hex.EncodeToString(hash[:])
        return "rnr" + address[:core.AddressHexLength], nil
}

func (w *Wallet) SignTransaction(tx *core.Transaction) error {
        txHash, err := tx.Hash()
        if err != nil {
                return fmt.Errorf("failed to hash transaction: %w", err)
        }

        r, s, err := ecdsa.Sign(rand.Reader, w.PrivateKey, txHash)
        if err != nil {
                return fmt.Errorf("failed to sign transaction: %w", err)
        }

        signature := append(r.Bytes(), s.Bytes()...)
        tx.Signature = signature

        return nil
}

func NewTransaction(from, to string, amount *big.Int, fee *big.Int, nonce uint64) *core.Transaction {
        tx := &core.Transaction{
                From:      from,
                To:        to,
                Amount:    amount,
                Timestamp: time.Now(),
                Fee:       fee,
                Nonce:     nonce,
        }
        txHash, _ := tx.Hash()
        tx.ID = hex.EncodeToString(txHash)
        return tx
}

func (w *Wallet) Encrypt(password string) ([]byte, error) {
        // SECURITY FIX: Generate unique random salt per encryption (not hardcoded)
        // This prevents rainbow table attacks and ensures different keys for same password
        salt := make([]byte, 32)
        if _, err := io.ReadFull(rand.Reader, salt); err != nil {
                return nil, fmt.Errorf("failed to generate salt: %w", err)
        }

        key, err := scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
        if err != nil {
                return nil, fmt.Errorf("failed to derive key: %w", err)
        }

        block, err := aes.NewCipher(key)
        if err != nil {
                return nil, fmt.Errorf("failed to create cipher: %w", err)
        }

        gcm, err := cipher.NewGCM(block)
        if err != nil {
                return nil, fmt.Errorf("failed to create GCM: %w", err)
        }

        nonce := make([]byte, gcm.NonceSize())
        if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
                return nil, fmt.Errorf("failed to generate nonce: %w", err)
        }

        privateKeyBytes := w.PrivateKey.D.Bytes()
        encrypted := gcm.Seal(nonce, nonce, privateKeyBytes, nil)

        // SECURITY: Prepend salt to encrypted data so Decrypt can extract it
        // Format: [32-byte salt][encrypted data]
        result := append(salt, encrypted...)

        return result, nil
}

func (w *Wallet) Decrypt(encryptedData []byte, password string) error {
        // SECURITY FIX: Extract salt from encrypted data (first 32 bytes)
        // Format: [32-byte salt][encrypted data]
        if len(encryptedData) < 32 {
                return fmt.Errorf("encrypted data too short to contain salt")
        }

        salt := encryptedData[:32]
        encrypted := encryptedData[32:]

        key, err := scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
        if err != nil {
                return fmt.Errorf("failed to derive key: %w", err)
        }

        block, err := aes.NewCipher(key)
        if err != nil {
                return fmt.Errorf("failed to create cipher: %w", err)
        }

        gcm, err := cipher.NewGCM(block)
        if err != nil {
                return fmt.Errorf("failed to create GCM: %w", err)
        }

        nonceSize := gcm.NonceSize()
        if len(encrypted) < nonceSize {
                return fmt.Errorf("encrypted data too short")
        }

        nonce, ciphertext := encrypted[:nonceSize], encrypted[nonceSize:]
        plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
        if err != nil {
                return fmt.Errorf("failed to decrypt: %w", err)
        }

        w.PrivateKey.D = new(big.Int).SetBytes(plaintext)
        w.PrivateKey.PublicKey.X, w.PrivateKey.PublicKey.Y = w.PrivateKey.PublicKey.Curve.ScalarBaseMult(plaintext)

        return nil
}
