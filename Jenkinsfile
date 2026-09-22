pipeline {
    agent any

    environment {
        IMAGE = "mafifdev/autoscale-go-k3s"
        NAMESPACE = "go-cicd"
        DEPLOYMENT = "go-app"
    }

    stages {

        stage('Checkout') {
            steps {
                checkout scm
            }
        }

        stage('Go Test') {
            steps {
                sh 'go test ./...'
            }
        }

        stage('Docker Build') {
            steps {
                sh '''
                    docker build \
                      -t ${IMAGE}:${BUILD_NUMBER} \
                      -t ${IMAGE}:latest .
                '''
            }
        }

        stage('Docker Push') {
            steps {
                withCredentials([
                    usernamePassword(
                        credentialsId: 'dockerhub',
                        usernameVariable: 'DOCKER_USERNAME',
                        passwordVariable: 'DOCKER_PASSWORD'
                    )
                ]) {
                    sh '''
                        echo "$DOCKER_PASSWORD" | docker login -u "$DOCKER_USERNAME" --password-stdin

                        docker push ${IMAGE}:${BUILD_NUMBER}
                        docker push ${IMAGE}:latest

                        docker logout
                    '''
                }
            }
        }

        stage('Deploy to K3s') {
            steps {
                sh '''
                    kubectl apply -f k8s/service.yaml
                    kubectl apply -f k8s/keda.yaml

                    kubectl set image deployment/${DEPLOYMENT} \
                      go-app=${IMAGE}:${BUILD_NUMBER} \
                      -n ${NAMESPACE}
                '''
            }
        }

        stage('Rollout Status') {
            steps {
                sh '''
                    kubectl rollout status deployment/${DEPLOYMENT} \
                      -n ${NAMESPACE} \
                      --timeout=120s
                '''
            }
        }

        stage('Health Check') {
            steps {
                sh '''
                    sleep 5
                    curl -f http://192.168.123.240:31048/health
                '''
            }
        }
    }
}
