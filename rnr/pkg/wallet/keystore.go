package wallet

import (
        "crypto/aes"
        "crypto/cipher"
        "crypto/ecdsa"
        "crypto/elliptic"
        "crypto/rand"
        "crypto/sha256"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "io"
        "math/big"
        "os"

        "golang.org/x/crypto/scrypt"
)

type KeystoreFile struct {
        Address    string `json:"address"`
        Crypto     Crypto `json:"crypto"`
        Mnemonic   string `json:"mnemonic"`
        Version    int    `json:"version"`
}

type Crypto struct {
        Cipher       string       `json:"cipher"`
        CipherText   string       `json:"ciphertext"`
        CipherParams CipherParams `json:"cipherparams"`
        KDF          string       `json:"kdf"`
        KDFParams    KDFParams    `json:"kdfparams"`
        MAC          string       `json:"mac"`
}

type CipherParams struct {
        IV string `json:"iv"`
}

type KDFParams struct {
        DKLen int    `json:"dklen"`
        N     int    `json:"n"`
        P     int    `json:"p"`
        R     int    `json:"r"`
        Salt  string `json:"salt"`
}

func SaveWalletToFile(w *Wallet, password, filepath string) error {
        salt := make([]byte, 32)
        if _, err := io.ReadFull(rand.Reader, salt); err != nil {
                return err
        }

        derivedKey, err := scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
        if err != nil {
                return err
        }

        encryptKey := derivedKey[:16]
        
        privateKeyBytes := w.PrivateKey.D.Bytes()
        
        iv := make([]byte, aes.BlockSize)
        if _, err := io.ReadFull(rand.Reader, iv); err != nil {
                return err
        }

        block, err := aes.NewCipher(encryptKey)
        if err != nil {
                return err
        }

        ciphertext := make([]byte, len(privateKeyBytes))
        stream := cipher.NewCTR(block, iv)
        stream.XORKeyStream(ciphertext, privateKeyBytes)

        mac := sha256.Sum256(append(derivedKey[16:32], ciphertext...))

        keystore := KeystoreFile{
                Address:  w.Address,
                Mnemonic: "", // Don't store mnemonic in keystore for security
                Version:  1,
                Crypto: Crypto{
                        Cipher:     "aes-128-ctr",
                        CipherText: hex.EncodeToString(ciphertext),
                        CipherParams: CipherParams{
                                IV: hex.EncodeToString(iv),
                        },
                        KDF: "scrypt",
                        KDFParams: KDFParams{
                                DKLen: 32,
                                N:     32768,
                                P:     1,
                                R:     8,
                                Salt:  hex.EncodeToString(salt),
                        },
                        MAC: hex.EncodeToString(mac[:]),
                },
        }

        data, err := json.MarshalIndent(keystore, "", "  ")
        if err != nil {
                return err
        }

        return os.WriteFile(filepath, data, 0600)
}

func LoadWalletFromFile(password, filepath string) (*Wallet, error) {
        data, err := os.ReadFile(filepath)
        if err != nil {
                return nil, err
        }

        var keystore KeystoreFile
        if err := json.Unmarshal(data, &keystore); err != nil {
                return nil, err
        }

        salt, err := hex.DecodeString(keystore.Crypto.KDFParams.Salt)
        if err != nil {
                return nil, err
        }

        derivedKey, err := scrypt.Key(
                []byte(password),
                salt,
                keystore.Crypto.KDFParams.N,
                keystore.Crypto.KDFParams.R,
                keystore.Crypto.KDFParams.P,
                keystore.Crypto.KDFParams.DKLen,
        )
        if err != nil {
                return nil, err
        }

        ciphertext, err := hex.DecodeString(keystore.Crypto.CipherText)
        if err != nil {
                return nil, err
        }

        mac := sha256.Sum256(append(derivedKey[16:32], ciphertext...))
        storedMAC, err := hex.DecodeString(keystore.Crypto.MAC)
        if err != nil {
                return nil, err
        }

        if hex.EncodeToString(mac[:]) != hex.EncodeToString(storedMAC) {
                return nil, fmt.Errorf("invalid password")
        }

        encryptKey := derivedKey[:16]
        iv, err := hex.DecodeString(keystore.Crypto.CipherParams.IV)
        if err != nil {
                return nil, err
        }

        block, err := aes.NewCipher(encryptKey)
        if err != nil {
                return nil, err
        }

        plaintext := make([]byte, len(ciphertext))
        stream := cipher.NewCTR(block, iv)
        stream.XORKeyStream(plaintext, ciphertext)

        privateKey := new(ecdsa.PrivateKey)
        privateKey.D = new(big.Int).SetBytes(plaintext)
        privateKey.PublicKey.Curve = elliptic.P256()
        privateKey.PublicKey.X, privateKey.PublicKey.Y = privateKey.PublicKey.Curve.ScalarBaseMult(plaintext)

        wallet := &Wallet{
                PrivateKey: privateKey,
                PublicKey:  &privateKey.PublicKey,
                Address:    keystore.Address,
        }

        return wallet, nil
}

func WalletExists(filepath string) bool {
        _, err := os.Stat(filepath)
        return err == nil
}
