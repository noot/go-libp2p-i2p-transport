package i2p

import (
	"context"
	"log"
	"testing"

	"github.com/eyedeekay/sam3"
	"github.com/eyedeekay/sam3/i2pkeys"

	crypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/sec"
	"github.com/libp2p/go-libp2p/core/sec/insecure"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/net/conngater"
	tptu "github.com/libp2p/go-libp2p/p2p/net/upgrader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const SAMHost = "127.0.0.1:7656"

func makeInsecureMuxer(t *testing.T) (peer.ID, sec.SecureTransport) {
	t.Helper()
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, 256)
	require.NoError(t, err)

	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(priv)
	require.NoError(t, err)

	st := insecure.NewWithIdentity(insecure.ID, id, priv)
	return id, st
}

type ServerInfo struct {
	Addr   i2pkeys.I2PAddr
	PeerID peer.ID
}

func TestBuildI2PTransport(t *testing.T) {
	ch := make(chan *ServerInfo, 1)
	go setupServer(t, ch)

	serverAddrAndPeer := <-ch
	setupClient(t, serverAddrAndPeer.Addr, serverAddrAndPeer.PeerID, 2345)

}

func setupClient(t *testing.T, serverAddr i2pkeys.I2PAddr, serverPeerID peer.ID, randNum int) {
	log.Println("Starting client setup")
	sam, err := sam3.NewSAM(SAMHost)
	if err != nil {
		assert.Fail(t, "Failed to connect to SAM")
		return
	}
	keys, err := sam.NewKeys()
	if err != nil {
		assert.Fail(t, "Failed to generate keys")
		return
	}

	builder, _, err := I2PTransportBuilder(sam, keys, "23459")
	assert.NoError(t, err)

	peerID, sm := makeInsecureMuxer(t)
	log.Println("Client Peer ID is: " + peerID.String())

	upgrader, err := tptu.New(
		[]sec.SecureTransport{sm},
		[]tptu.StreamMuxer{{
			ID:    yamux.ID,
			Muxer: new(yamux.Transport),
		}},
		nil,
		nil,
		&conngater.BasicConnectionGater{},
	)
	assert.NoError(t, err)
	secureTransport, err := builder(upgrader)
	assert.NoError(t, err)

	serverMultiAddr, err := I2PAddrToMultiAddr(string(serverAddr))
	assert.NoError(t, err)
	log.Println("Dialing host on this destination: " + serverMultiAddr.String())

	for i := 0; i < 5; i++ {
		log.Println("Starting dial")
		conn, err := secureTransport.Dial(context.TODO(), serverMultiAddr, serverPeerID)
		if err != nil {
			assert.Fail(t, "Failed to dial", err)
			return
		}
		log.Println("Opening Stream")
		stream, err := conn.OpenStream(context.TODO())
		if err != nil {
			assert.Fail(t, "Failed to open outbound stream", err)
			return
		}

		_, err = stream.Write([]byte("Hello!"))
		assert.NoError(t, err)
		stream.Close()
	}
}

func setupServer(t *testing.T, addrChan chan *ServerInfo) {
	sam, err := sam3.NewSAM(SAMHost)
	if err != nil {
		assert.Fail(t, "Failed to connect to SAM", err)
		addrChan <- nil
		return
	}
	keys, err := sam.NewKeys()
	if err != nil {
		assert.Fail(t, "Failed to generate keys", err)
		addrChan <- nil
		return
	}

	port := "45793"
	builder, listenAddr, err := I2PTransportBuilder(sam, keys, port)
	assert.NoError(t, err)

	peerID, sm := makeInsecureMuxer(t)
	log.Println("Server Peer ID is: " + peerID.String())

	upgrader, err := tptu.New(
		[]sec.SecureTransport{sm},
		[]tptu.StreamMuxer{{
			ID:    yamux.ID,
			Muxer: new(yamux.Transport),
		}},
		nil,
		nil,
		&conngater.BasicConnectionGater{},
	)
	assert.NoError(t, err)
	secureTransport, err := builder(upgrader)
	assert.NoError(t, err)

	listener, err := secureTransport.Listen(listenAddr)
	if err != nil {
		assert.Fail(t, "Failed to create listener", err)
		addrChan <- nil
		return
	}

	serverInfo := &ServerInfo{
		i2pkeys.I2PAddr(listener.Addr().String()),
		peerID,
	}
	addrChan <- serverInfo
	log.Println("Listener Addr: " + listener.Addr().String())

	for i := 0; i < 5; i++ {
		capableConnection, err := listener.Accept()
		if err != nil {
			assert.Fail(t, "Failed to accept connection: "+err.Error())
		}

		stream, err := capableConnection.AcceptStream()
		assert.NoError(t, err)

		buf := make([]byte, 1024)
		_, err = stream.Read(buf)
		assert.NoError(t, err)
		_, err = stream.Write([]byte(capableConnection.LocalMultiaddr().String()))
		assert.NoError(t, err)
		log.Println(capableConnection.RemoteMultiaddr())

		stream.Close()
	}
}
