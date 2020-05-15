package main

import (
	"fmt"
	"io"
	"math/rand"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v2"
	"github.com/pion/webrtc/v2/examples/internal/signal"
)

func main() {
	// Everything below is the Pion WebRTC API! Thanks for using it ❤️.

	// Wait for the offer to be pasted
	offer := webrtc.SessionDescription{}
	signal.Decode(signal.MustReadStdin(), &offer)

	// We make our own mediaEngine so we can place the sender's codecs in it. Since we are echoing their RTP packet
	// back to them we are actually codec agnostic - we can accept all their codecs. This also ensures that we use the
	// dynamic media type from the sender in our answer.
	mediaEngine := webrtc.MediaEngine{}

	// Add codecs to the mediaEngine. Note that even though we are only going to echo back the sender's video we also
	// add audio codecs. This is because createAnswer will create an audioTransceiver and associated SDP and we currently
	// cannot tell it not to. The audio SDP must match the sender's codecs too...
	err := mediaEngine.PopulateFromSDP(offer)
	if err != nil {
		panic(err)
	}

	videoCodecs := mediaEngine.GetCodecsByKind(webrtc.RTPCodecTypeVideo)
	if len(videoCodecs) == 0 {
		panic("Offer contained no video codecs")
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
	// Create a new RTCPeerConnection
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}

	outputTracks := map[string]*webrtc.Track{}

	// Create Track that we send video back to browser on
	outputTrack, err := peerConnection.NewTrack(videoCodecs[0].PayloadType, rand.Uint32(), "video_q", "pion_q")
	if err != nil {
		panic(err)
	}
	outputTracks["q"] = outputTrack

	outputTrack, err = peerConnection.NewTrack(videoCodecs[0].PayloadType, rand.Uint32(), "video_h", "pion_h")
	if err != nil {
		panic(err)
	}
	outputTracks["h"] = outputTrack

	outputTrack, err = peerConnection.NewTrack(videoCodecs[0].PayloadType, rand.Uint32(), "video_f", "pion_f")
	if err != nil {
		panic(err)
	}
	outputTracks["f"] = outputTrack

	// Add this newly created track to the PeerConnection
	if _, err = peerConnection.AddTrack(outputTracks["q"]); err != nil {
		panic(err)
	}
	if _, err = peerConnection.AddTrack(outputTracks["h"]); err != nil {
		panic(err)
	}
	if _, err = peerConnection.AddTrack(outputTracks["f"]); err != nil {
		panic(err)
	}

	fmt.Printf("offer: %s\n", offer.SDP)
	// Set the remote SessionDescription
	err = peerConnection.SetRemoteDescription(offer)
	if err != nil {
		panic(err)
	}

	// Set a handler for when a new remote track starts
	peerConnection.OnTrack(func(track *webrtc.Track, receiver *webrtc.RTPReceiver) {
		fmt.Printf("Track has started\n")

		// Whether this track is the one currently sending to the channel (on change
		// of this we send a PLI to have the entire picture updated)
		for _, inStream := range track.Streams() {
			go func(inStream *webrtc.TrackRTPStream) {
				rid := inStream.RID()
				go func() {
					ticker := time.NewTicker(1 * time.Second)
					for {
						select {
						case <-ticker.C:
							fmt.Printf("Sending pli for ssrc: %d\n", inStream.SSRC())
							if writeErr := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: inStream.SSRC()}}); writeErr != nil {
								fmt.Println(writeErr)
							}
							if writeErr := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.ReceiverEstimatedMaximumBitrate{Bitrate: 10000000, SenderSSRC: inStream.SSRC()}}); writeErr != nil {
								fmt.Println(writeErr)
							}
						}
					}
				}()
				for {
					// Read RTP packets being sent to Pion
					packet, err := inStream.ReadRTP()
					if err != nil {
						panic(err)
					}

					packet.SSRC = outputTracks[rid].SSRC()

					if err := outputTracks[rid].WriteRTP(packet); err != nil && err != io.ErrClosedPipe {
						panic(err)
					}
				}
			}(inStream)
		}
	})
	// Set the handler for ICE connection state and update chan if connected
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("Connection State has changed %s \n", connectionState.String())
	})

	// Create an answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	fmt.Printf("answer: %s\n", answer.SDP)

	// Sets the LocalDescription, and starts our UDP listeners
	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		panic(err)
	}

	// Output the answer in base64 so we can paste it in browser
	fmt.Printf("Paste below base64 in browser:\n%v\n", signal.Encode(answer))

	// Block forever
	select {}
}
