"""Check authenticated practice, anonymous grading, and MicroK8s persistence.

Uses the shared harness and removes only its temporary account and lesson.
Stop the app port forward before running this check.
"""
from microk8s import test

if __name__ == '__main__':
    test()
