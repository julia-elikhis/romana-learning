"""Check PDF text extraction in the local application container using synthetic text."""
from microk8s import kube, namespace

content = b'BT /F1 12 Tf 72 720 Td (Eu sunt acasa in fiecare zi.) Tj ET'
objects = [
    b'<< /Type /Catalog /Pages 2 0 R >>',
    b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
    b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
    b'<< /Length ' + str(len(content)).encode() + b' >>\nstream\n' + content + b'\nendstream',
]
pdf = b'%PDF-1.4\n'
offsets = []
for index, obj in enumerate(objects, 1):
    offsets.append(len(pdf))
    pdf += str(index).encode() + b' 0 obj\n' + obj + b'\nendobj\n'
xref = len(pdf)
pdf += b'xref\n0 6\n0000000000 65535 f \n'
for offset in offsets:
    pdf += f'{offset:010d} 00000 n \n'.encode()
pdf += b'trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n' + str(xref).encode() + b'\n%%EOF\n'
namespace()
result = kube('exec', '-i', 'deployment/practice-romanian', '--', 'pdftotext', '-layout', '-', '-', data=pdf)
assert b'Eu sunt acasa' in result.stdout
print('PASS: PDF text extraction in the application image')
