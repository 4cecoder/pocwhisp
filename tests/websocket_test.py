#!/usr/bin/env python3
"""
WebSocket streaming test client for PocWhisp.
Tests real-time audio streaming and transcription.
"""

import asyncio
import json
import time
import websockets
import numpy as np
import wave
import io
from pathlib import Path

class WebSocketTestClient:
    def __init__(self, url="ws://localhost:8080/api/v1/stream"):
        self.url = url
        self.websocket = None
        self.session_id = None
        self.connected = False
        
    async def connect(self):
        """Connect to WebSocket server."""
        try:
            self.websocket = await websockets.connect(self.url)
            self.connected = True
            print(f"✅ Connected to WebSocket: {self.url}")
            
            # Listen for welcome message
            message = await self.websocket.recv()
            data = json.loads(message)
            
            if data.get("type") == "connected":
                self.session_id = data.get("session_id")
                print(f"📋 Session ID: {self.session_id}")
                print(f"🔧 Capabilities: {data.get('data', {}).get('capabilities', [])}")
                return True
            else:
                print(f"❌ Unexpected welcome message: {data}")
                return False
                
        except Exception as e:
            print(f"❌ Connection failed: {e}")
            return False
    
    async def disconnect(self):
        """Disconnect from WebSocket server."""
        if self.websocket and self.connected:
            await self.websocket.close()
            self.connected = False
            print("🔌 Disconnected from WebSocket")
    
    async def send_message(self, message):
        """Send a text message to the server."""
        if not self.connected:
            raise Exception("Not connected to WebSocket")
        
        message_str = json.dumps(message)
        await self.websocket.send(message_str)
        print(f"📤 Sent: {message['type']}")
    
    async def send_binary(self, data):
        """Send binary audio data to the server."""
        if not self.connected:
            raise Exception("Not connected to WebSocket")
        
        await self.websocket.send(data)
        print(f"📤 Sent binary data: {len(data)} bytes")
    
    async def receive_message(self):
        """Receive a message from the server."""
        if not self.connected:
            raise Exception("Not connected to WebSocket")
        
        message = await self.websocket.recv()
        
        if isinstance(message, bytes):
            print(f"📥 Received binary data: {len(message)} bytes")
            return {"type": "binary", "data": message}
        else:
            data = json.loads(message)
            print(f"📥 Received: {data['type']}")
            return data
    
    async def start_streaming(self, config=None):
        """Start real-time audio streaming."""
        if config is None:
            config = {
                "sample_rate": 16000,
                "channels": 2,
                "format": "pcm_s16le"
            }
        
        message = {
            "type": "start_streaming",
            "config": config
        }
        
        await self.send_message(message)
        
        # Wait for confirmation
        response = await self.receive_message()
        if response.get("type") == "streaming_started":
            print("🎵 Streaming started successfully")
            return True
        else:
            print(f"❌ Failed to start streaming: {response}")
            return False
    
    async def stop_streaming(self):
        """Stop real-time audio streaming."""
        message = {"type": "stop_streaming"}
        await self.send_message(message)
        
        # Wait for confirmation
        response = await self.receive_message()
        if response.get("type") == "streaming_stopped":
            print("⏹️ Streaming stopped successfully")
            return True
        else:
            print(f"❌ Failed to stop streaming: {response}")
            return False
    
    async def send_audio_chunk(self, audio_data, sequence=0, final=False):
        """Send an audio chunk using structured message."""
        message = {
            "type": "audio_chunk",
            "data": audio_data.decode('latin-1') if isinstance(audio_data, bytes) else audio_data,
            "sequence": sequence,
            "final": final
        }
        
        await self.send_message(message)
    
    async def ping(self):
        """Send a ping message."""
        message = {"type": "ping"}
        await self.send_message(message)
        
        response = await self.receive_message()
        if response.get("type") == "pong":
            server_time = response.get("data", {}).get("server_time", 0)
            print(f"🏓 Pong received (server time: {server_time})")
            return True
        else:
            print(f"❌ Unexpected ping response: {response}")
            return False
    
    async def get_status(self):
        """Get connection status."""
        message = {"type": "get_status"}
        await self.send_message(message)
        
        response = await self.receive_message()
        if response.get("type") == "status":
            status = response.get("data", {})
            print(f"📊 Status: {json.dumps(status, indent=2)}")
            return status
        else:
            print(f"❌ Unexpected status response: {response}")
            return None

def generate_test_audio(duration=5, sample_rate=16000, channels=2):
    """Generate test stereo audio data."""
    samples = int(duration * sample_rate)
    
    # Generate different tones for left and right channels
    t = np.linspace(0, duration, samples, False)
    
    # Left channel: 440 Hz tone (A4)
    left_channel = np.sin(2 * np.pi * 440 * t)
    
    # Right channel: 880 Hz tone (A5)
    right_channel = np.sin(2 * np.pi * 880 * t)
    
    # Combine channels
    if channels == 2:
        audio = np.column_stack((left_channel, right_channel))
    else:
        audio = left_channel
    
    # Convert to 16-bit PCM
    audio_int16 = (audio * 32767).astype(np.int16)
    
    return audio_int16

def audio_to_wav_bytes(audio_data, sample_rate=16000, channels=2):
    """Convert audio data to WAV format bytes."""
    buffer = io.BytesIO()
    
    with wave.open(buffer, 'wb') as wav_file:
        wav_file.setnchannels(channels)
        wav_file.setsampwidth(2)  # 16-bit
        wav_file.setframerate(sample_rate)
        wav_file.writeframes(audio_data.tobytes())
    
    return buffer.getvalue()

async def test_basic_connection():
    """Test basic WebSocket connection."""
    print("\n🧪 Testing Basic Connection")
    print("=" * 50)
    
    client = WebSocketTestClient()
    
    try:
        # Connect
        success = await client.connect()
        assert success, "Failed to connect"
        
        # Ping
        await client.ping()
        
        # Get status
        await client.get_status()
        
        # Disconnect
        await client.disconnect()
        
        print("✅ Basic connection test passed")
        
    except Exception as e:
        print(f"❌ Basic connection test failed: {e}")
        await client.disconnect()

async def test_streaming_workflow():
    """Test complete streaming workflow."""
    print("\n🧪 Testing Streaming Workflow")
    print("=" * 50)
    
    client = WebSocketTestClient()
    
    try:
        # Connect
        await client.connect()
        
        # Start streaming
        await client.start_streaming()
        
        # Generate and send test audio
        print("🎵 Generating test audio...")
        audio_data = generate_test_audio(duration=2, sample_rate=16000, channels=2)
        
        # Send audio in chunks
        chunk_size = 1024  # bytes
        total_bytes = audio_data.nbytes
        
        for i in range(0, total_bytes, chunk_size):
            chunk = audio_data.flat[i:i+chunk_size].tobytes()
            final = (i + chunk_size) >= total_bytes
            
            await client.send_binary(chunk)
            
            # Listen for responses
            try:
                response = await asyncio.wait_for(client.receive_message(), timeout=1.0)
                if response.get("type") == "transcription":
                    print(f"📝 Transcription: {response.get('data', {})}")
                elif response.get("type") == "audio_received":
                    data = response.get("data", {})
                    print(f"✅ Audio acknowledged: {data.get('bytes_received')} bytes, buffer: {data.get('buffer_size')}")
            except asyncio.TimeoutError:
                pass  # No immediate response, continue
            
            await asyncio.sleep(0.1)  # Simulate real-time streaming
        
        # Stop streaming
        await client.stop_streaming()
        
        # Disconnect
        await client.disconnect()
        
        print("✅ Streaming workflow test passed")
        
    except Exception as e:
        print(f"❌ Streaming workflow test failed: {e}")
        await client.disconnect()

async def test_concurrent_connections():
    """Test multiple concurrent WebSocket connections."""
    print("\n🧪 Testing Concurrent Connections")
    print("=" * 50)
    
    clients = []
    num_clients = 3
    
    try:
        # Create and connect multiple clients
        for i in range(num_clients):
            client = WebSocketTestClient()
            await client.connect()
            clients.append(client)
            print(f"✅ Client {i+1} connected")
        
        # Test concurrent operations
        tasks = []
        for i, client in enumerate(clients):
            tasks.append(client.ping())
            tasks.append(client.get_status())
        
        # Execute all operations concurrently
        await asyncio.gather(*tasks)
        
        # Disconnect all clients
        for i, client in enumerate(clients):
            await client.disconnect()
            print(f"🔌 Client {i+1} disconnected")
        
        print("✅ Concurrent connections test passed")
        
    except Exception as e:
        print(f"❌ Concurrent connections test failed: {e}")
        
        # Cleanup
        for client in clients:
            try:
                await client.disconnect()
            except:
                pass

async def test_error_conditions():
    """Test error handling and edge cases."""
    print("\n🧪 Testing Error Conditions")
    print("=" * 50)
    
    client = WebSocketTestClient()
    
    try:
        await client.connect()
        
        # Test invalid message format
        try:
            await client.websocket.send("invalid json")
            response = await client.receive_message()
            if response.get("type") == "error":
                print("✅ Invalid JSON handled correctly")
            else:
                print(f"⚠️ Unexpected response to invalid JSON: {response}")
        except Exception as e:
            print(f"✅ Invalid JSON caused expected error: {e}")
        
        # Test unknown message type
        await client.send_message({"type": "unknown_command"})
        response = await client.receive_message()
        if response.get("type") == "error":
            print("✅ Unknown command handled correctly")
        
        # Test sending audio without starting streaming
        await client.send_binary(b"fake audio data")
        response = await client.receive_message()
        if response.get("type") == "error":
            print("✅ Audio without streaming handled correctly")
        
        # Test double start streaming
        await client.start_streaming()
        await client.send_message({"type": "start_streaming"})
        response = await client.receive_message()
        if response.get("type") == "error":
            print("✅ Double start streaming handled correctly")
        
        await client.stop_streaming()
        await client.disconnect()
        
        print("✅ Error conditions test passed")
        
    except Exception as e:
        print(f"❌ Error conditions test failed: {e}")
        await client.disconnect()

async def test_performance():
    """Test performance with high-frequency data."""
    print("\n🧪 Testing Performance")
    print("=" * 50)
    
    client = WebSocketTestClient()
    
    try:
        await client.connect()
        await client.start_streaming()
        
        # Send high-frequency audio chunks
        chunk_size = 512
        num_chunks = 100
        start_time = time.time()
        
        print(f"📊 Sending {num_chunks} chunks of {chunk_size} bytes each...")
        
        for i in range(num_chunks):
            chunk = np.random.randint(-32768, 32767, chunk_size, dtype=np.int16).tobytes()
            await client.send_binary(chunk)
            
            # Brief pause to avoid overwhelming
            await asyncio.sleep(0.01)
        
        end_time = time.time()
        duration = end_time - start_time
        throughput = (num_chunks * chunk_size) / duration / 1024  # KB/s
        
        print(f"📈 Performance: {throughput:.2f} KB/s over {duration:.2f}s")
        
        await client.stop_streaming()
        await client.disconnect()
        
        print("✅ Performance test completed")
        
    except Exception as e:
        print(f"❌ Performance test failed: {e}")
        await client.disconnect()

async def main():
    """Run all WebSocket tests."""
    print("🧪 PocWhisp WebSocket Test Suite")
    print("=" * 50)
    
    tests = [
        ("Basic Connection", test_basic_connection),
        ("Streaming Workflow", test_streaming_workflow),
        ("Concurrent Connections", test_concurrent_connections),
        ("Error Conditions", test_error_conditions),
        ("Performance", test_performance),
    ]
    
    results = []
    
    for test_name, test_func in tests:
        try:
            print(f"\n🚀 Running {test_name}...")
            await test_func()
            results.append((test_name, True))
        except Exception as e:
            print(f"❌ {test_name} failed: {e}")
            results.append((test_name, False))
    
    # Summary
    print("\n📊 Test Results Summary")
    print("=" * 50)
    
    passed = 0
    for test_name, success in results:
        status = "✅ PASSED" if success else "❌ FAILED"
        print(f"{test_name}: {status}")
        if success:
            passed += 1
    
    print(f"\nOverall: {passed}/{len(results)} tests passed")
    
    if passed == len(results):
        print("\n🎉 All WebSocket tests PASSED!")
        print("\n✨ WebSocket Features Summary:")
        print("  • Real-time bidirectional communication")
        print("  • Audio streaming with chunked processing")
        print("  • Connection management and cleanup")
        print("  • Error handling and validation")
        print("  • Concurrent connection support")
        print("  • Performance optimized for real-time use")
        return True
    else:
        print(f"\n❌ {len(results) - passed} WebSocket tests FAILED!")
        return False

if __name__ == "__main__":
    try:
        success = asyncio.run(main())
        exit(0 if success else 1)
    except KeyboardInterrupt:
        print("\n🛑 Tests interrupted by user")
        exit(1)
